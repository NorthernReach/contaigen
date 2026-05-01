package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewTextLogger(t *testing.T) {
	var output bytes.Buffer

	logger, err := New(Config{
		Level:  "debug",
		Format: "text",
		Output: &output,
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	logger.Debug("hello", slog.String("component", "test"))
	if !strings.Contains(output.String(), "component=test") {
		t.Fatalf("expected structured text output, got %q", output.String())
	}
}

func TestNewRejectsInvalidLevel(t *testing.T) {
	_, err := New(Config{Level: "loud"})
	if err == nil {
		t.Fatal("expected invalid level error")
	}
}
