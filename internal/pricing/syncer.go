package pricing

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// SyncerConfig controls remote catalog synchronization.
type SyncerConfig struct {
	CatalogURL string
	Interval   time.Duration
	HTTPClient *http.Client
}

// Syncer manages background periodic fetching of external model pricing catalogs.
type Syncer struct {
	catalog    *Catalog
	catalogURL string
	interval   time.Duration
	client     *http.Client
}

// NewSyncer constructs a Syncer targeting a Catalog.
func NewSyncer(catalog *Catalog, cfg SyncerConfig) *Syncer {
	if catalog == nil {
		catalog = DefaultCatalog()
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
		}
	}
	return &Syncer{
		catalog:    catalog,
		catalogURL: cfg.CatalogURL,
		interval:   interval,
		client:     client,
	}
}

// Start launches the background synchronization loop.
// It performs an immediate sync and then updates every configured interval until ctx is cancelled.
func (s *Syncer) Start(ctx context.Context) {
	if s.catalogURL == "" {
		return
	}

	go func() {
		slog.Info("starting model pricing catalog background syncer",
			slog.String("url", s.catalogURL),
			slog.Duration("interval", s.interval),
		)

		// Initial sync with short backoff on failure
		if err := s.SyncOnce(ctx); err != nil {
			slog.Warn("initial model pricing sync failed, using embedded baseline",
				slog.String("error", err.Error()),
			)
		}

		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("model pricing catalog syncer stopped")
				return
			case <-ticker.C:
				if err := s.SyncOnce(ctx); err != nil {
					slog.Warn("periodic model pricing sync failed",
						slog.String("error", err.Error()),
					)
				}
			}
		}
	}()
}

// SyncOnce fetches and applies the pricing catalog once.
func (s *Syncer) SyncOnce(ctx context.Context) error {
	if s.catalogURL == "" {
		return nil
	}

	var reader io.ReadCloser

	if strings.HasPrefix(s.catalogURL, "file://") {
		filePath := strings.TrimPrefix(s.catalogURL, "file://")
		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("open pricing file %s: %w", filePath, err)
		}
		reader = f
	} else {
		parsed, err := url.Parse(s.catalogURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("unsupported catalog url scheme: %s", s.catalogURL)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.catalogURL, nil)
		if err != nil {
			return fmt.Errorf("create catalog request: %w", err)
		}
		req.Header.Set("User-Agent", "Beavend-Pricing-Syncer/1.0")

		resp, err := s.client.Do(req)
		if err != nil {
			return fmt.Errorf("fetch catalog: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return fmt.Errorf("fetch catalog returned status %d", resp.StatusCode)
		}
		reader = resp.Body
	}
	defer reader.Close()

	const maxCatalogBytes = 64 << 20 // 64 MB max
	limited := io.LimitReader(reader, maxCatalogBytes)

	prices, err := ParseCatalogData(limited)
	if err != nil {
		return fmt.Errorf("parse pricing catalog data: %w", err)
	}

	if len(prices) == 0 {
		return fmt.Errorf("catalog contained zero valid model prices")
	}

	s.catalog.Update(prices)
	slog.Info("model pricing catalog successfully synced",
		slog.Int("model_count", len(prices)),
	)
	return nil
}
