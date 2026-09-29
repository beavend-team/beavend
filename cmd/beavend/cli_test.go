package main

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/beavend/beavend/internal/configcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_ExportMissingProjectFlag(t *testing.T) {
	err := runExport([]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--project is required")
}

func TestCLI_ImportMissingFileFlag(t *testing.T) {
	err := runImport([]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--file is required")
}

func TestCLI_PrintDiffReport(t *testing.T) {
	diff := &configcode.DiffReport{
		ProjectID:   "proj_test",
		Environment: "staging",
		Resources: []configcode.ResourceDiff{
			{
				Type:   "app",
				ID:     "my_app",
				Action: configcode.ActionCreate,
			},
			{
				Type:    "prompt",
				ID:      "my_prompt",
				Action:  configcode.ActionUpdate,
				Changes: []string{"template updated"},
			},
			{
				Type:   "policy",
				ID:     "my_policy",
				Action: configcode.ActionUnchanged,
			},
		},
		Summary: configcode.DiffSummary{
			TotalCreated:   1,
			TotalUpdated:   1,
			TotalUnchanged: 1,
		},
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printDiffReport(diff)

	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "Diff Plan for Project: proj_test (Environment: staging)")
	assert.Contains(t, output, "[CREATE]    app        my_app")
	assert.Contains(t, output, "[UPDATE]    prompt     my_prompt")
	assert.Contains(t, output, "template updated")
	assert.Contains(t, output, "[UNCHANGED] policy     my_policy")
	assert.Contains(t, output, "Summary: 1 to create, 1 to update, 1 unchanged")
}
