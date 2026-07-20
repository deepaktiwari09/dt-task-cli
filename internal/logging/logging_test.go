package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoggerSilentOutsideDevelopment(t *testing.T) {
	t.Setenv("DT_TASK_ENV", "")
	var output bytes.Buffer
	logger := New(&output)
	logger.Info("hidden", "task_id", 1)
	if output.Len() != 0 {
		t.Fatalf("unexpected production log: %s", output.String())
	}
}

func TestLoggerDevelopmentJSON(t *testing.T) {
	t.Setenv("DT_TASK_ENV", "development")
	t.Setenv("DT_TASK_LOG_LEVEL", "info")
	var output bytes.Buffer
	logger := New(&output)
	logger.Info("task created", "task_id", 1)
	if !strings.Contains(output.String(), `"task_id":1`) {
		t.Fatalf("missing structured field: %s", output.String())
	}
}
