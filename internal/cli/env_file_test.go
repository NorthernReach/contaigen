package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.env")
	if err := os.WriteFile(path, []byte(`
# full-line comments are ignored
FILE_TOKEN=from-file
EMPTY=
QUOTED="hello world"
SINGLE='one two'
export EXPORTED=yes
PASSWORD=abc#123
`), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	values, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("parse env file: %v", err)
	}
	got := strings.Join(values, ",")
	want := "FILE_TOKEN=from-file,EMPTY=,QUOTED=hello world,SINGLE=one two,EXPORTED=yes,PASSWORD=abc#123"
	if got != want {
		t.Fatalf("unexpected env values:\nwant: %s\n got: %s", want, got)
	}
}

func TestParseEnvFileRejectsInvalidLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.env")
	if err := os.WriteFile(path, []byte("NOPE\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	_, err := parseEnvFile(path)
	if err == nil {
		t.Fatal("expected invalid env file line")
	}
	if !strings.Contains(err.Error(), "line 1 must be KEY=VALUE") {
		t.Fatalf("unexpected error: %v", err)
	}
}
