package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/beavend/beavend/internal/config"
	"github.com/beavend/beavend/internal/configcode"
	"github.com/beavend/beavend/internal/secrets"
	synckit "github.com/beavend/beavend/internal/snapshot/sync"
	"github.com/beavend/beavend/internal/store"
	"github.com/redis/go-redis/v9"
)

func runExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	projectFlag := fs.String("project", "", "Project ID to export (required)")
	envFlag := fs.String("env", "production", "Environment name (default: production)")
	outputFlag := fs.String("output", "", "Output file path (default: stdout)")
	configFlag := fs.String("config", "", "Path to YAML configuration file")
	dbURLFlag := fs.String("db-url", "", "PostgreSQL database connection URL override")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *projectFlag == "" {
		return fmt.Errorf("--project is required")
	}

	cfg, err := loadCLIConfig(*configFlag, *dbURLFlag)
	if err != nil {
		return err
	}

	ctx := context.Background()
	st, err := store.NewStore(ctx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer st.Close()

	masterKey := secrets.DeriveKey(cfg.Security.MasterKey)
	engine := configcode.NewEngine(st.Queries(), nil, masterKey)

	doc, err := engine.Export(ctx, *projectFlag, *envFlag)
	if err != nil {
		return fmt.Errorf("export project: %w", err)
	}

	yamlBytes, err := doc.ToYAML()
	if err != nil {
		return fmt.Errorf("serialize yaml: %w", err)
	}

	if *outputFlag != "" {
		if err := os.WriteFile(*outputFlag, yamlBytes, 0644); err != nil {
			return fmt.Errorf("write output file %s: %w", *outputFlag, err)
		}
		fmt.Fprintf(os.Stderr, "Configuration exported successfully to %s\n", *outputFlag)
	} else {
		fmt.Println(string(yamlBytes))
	}
	return nil
}

func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	projectFlag := fs.String("project", "", "Project ID override (optional)")
	envFlag := fs.String("env", "", "Environment name override (optional)")
	fileFlag := fs.String("file", "", "Path to YAML configuration file, or '-' for stdin (required)")
	dryRunFlag := fs.Bool("dry-run", false, "Preview changes without modifying the database")
	configFlag := fs.String("config", "", "Path to YAML configuration file")
	dbURLFlag := fs.String("db-url", "", "PostgreSQL database connection URL override")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *fileFlag == "" {
		return fmt.Errorf("--file is required (use '-' for stdin)")
	}

	var data []byte
	var err error
	if *fileFlag == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(*fileFlag)
	}
	if err != nil {
		return fmt.Errorf("read file %s: %w", *fileFlag, err)
	}

	doc, err := configcode.FromYAML(data)
	if err != nil {
		return fmt.Errorf("parse yaml: %w", err)
	}

	if *projectFlag != "" {
		doc.Project.ID = *projectFlag
	}
	if *envFlag != "" {
		doc.Environment = *envFlag
	}

	if err := doc.Validate(); err != nil {
		return fmt.Errorf("invalid document: %w", err)
	}

	cfg, err := loadCLIConfig(*configFlag, *dbURLFlag)
	if err != nil {
		return err
	}

	ctx := context.Background()
	st, err := store.NewStore(ctx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer st.Close()

	masterKey := secrets.DeriveKey(cfg.Security.MasterKey)
	var publisher synckit.Publisher
	if cfg.Redis.URL != "" {
		rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.URL})
		publisher = synckit.NewRedisPublisher(rdb)
	}

	engine := configcode.NewEngine(st.Queries(), publisher, masterKey)

	if *dryRunFlag {
		diff, err := engine.Diff(ctx, doc)
		if err != nil {
			return fmt.Errorf("compute diff: %w", err)
		}
		printDiffReport(diff)
		return nil
	}

	report, err := engine.Apply(ctx, doc)
	if err != nil {
		return fmt.Errorf("apply configuration: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Configuration applied successfully to project %q (environment: %q):\n", doc.Project.ID, doc.Environment)
	fmt.Fprintf(os.Stderr, "  Created:   %d\n", report.Summary.TotalCreated)
	fmt.Fprintf(os.Stderr, "  Updated:   %d\n", report.Summary.TotalUpdated)
	fmt.Fprintf(os.Stderr, "  Unchanged: %d\n", report.Summary.TotalUnchanged)
	return nil
}

func loadCLIConfig(configFile, dbURLOverride string) (*config.Config, error) {
	var (
		cfg *config.Config
		err error
	)
	if configFile != "" {
		cfg, err = config.LoadFile(configFile)
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if dbURLOverride != "" {
		cfg.Database.URL = dbURLOverride
	}
	if cfg.Database.URL == "" {
		return nil, fmt.Errorf("database URL is required (specify via BEAVEND_DATABASE_URL, config file, or --db-url)")
	}

	return cfg, nil
}

func printDiffReport(diff *configcode.DiffReport) {
	fmt.Printf("Diff Plan for Project: %s (Environment: %s)\n", diff.ProjectID, diff.Environment)
	fmt.Println(strings.Repeat("-", 60))
	for _, r := range diff.Resources {
		tag := "[UNCHANGED]"
		switch r.Action {
		case configcode.ActionCreate:
			tag = "[CREATE]   "
		case configcode.ActionUpdate:
			tag = "[UPDATE]   "
		}

		fmt.Printf("%s %-10s %s\n", tag, r.Type, r.ID)
		for _, c := range r.Changes {
			fmt.Printf("             └─ %s\n", c)
		}
	}
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("Summary: %d to create, %d to update, %d unchanged\n",
		diff.Summary.TotalCreated,
		diff.Summary.TotalUpdated,
		diff.Summary.TotalUnchanged,
	)
}
