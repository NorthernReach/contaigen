package composex

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NorthernReach/contaigen/internal/model"
)

func TestParseFileSummarizesServices(t *testing.T) {
	path := writeComposeFile(t, `name: demo
services:
  web:
    image: nginx:alpine
  api:
    build: .
`)

	project, err := ParseFile(path)
	if err != nil {
		t.Fatalf("parse compose file: %v", err)
	}
	if project.Name != "demo" || len(project.Services) != 2 {
		t.Fatalf("unexpected project summary: %#v", project)
	}
	if project.Services[0].Name != "api" || !project.Services[0].HasBuild {
		t.Fatalf("services were not sorted/summarized: %#v", project.Services)
	}
	if project.Services[1].Name != "web" || project.Services[1].Image != "nginx:alpine" {
		t.Fatalf("unexpected web service: %#v", project.Services[1])
	}
}

func TestParseFileRequiresServiceImageOrBuild(t *testing.T) {
	path := writeComposeFile(t, `services:
  web:
    environment:
      A: B
`)

	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "must define image or build") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpInvokesDockerComposeWithNetworkOverride(t *testing.T) {
	path := writeComposeFile(t, `services:
  web:
    image: nginx:alpine
`)
	runner := &recordingRunner{}
	compose := New(WithRunner(runner))

	err := compose.Up(context.Background(), UpRequest{
		File:        path,
		ProjectName: "client-a-app",
		EnvName:     "lab",
		NetworkName: "client-a",
		Detach:      true,
	}, emptyStreams())
	if err != nil {
		t.Fatalf("compose up: %v", err)
	}
	if runner.binary != "docker" {
		t.Fatalf("unexpected binary: %q", runner.binary)
	}
	if !containsAll(runner.args, "compose", "-p", "client-a-app", "up", "-d") {
		t.Fatalf("unexpected compose args: %#v", runner.args)
	}
	if !containsAll(runner.env, "CONTAIGEN_ENV=lab", "CONTAIGEN_NETWORK=client-a") {
		t.Fatalf("unexpected env: %#v", runner.env)
	}
	override := runner.argAfter("-f", 2)
	if override == "" {
		t.Fatalf("expected second compose -f override in args: %#v", runner.args)
	}
	if !strings.Contains(runner.override, "io.contaigen.service: web") || !strings.Contains(runner.override, "name: client-a") {
		t.Fatalf("override did not label/connect service:\n%s", runner.override)
	}
}

func writeComposeFile(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "compose.yaml")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	return path
}

type recordingRunner struct {
	binary   string
	args     []string
	env      []string
	override string
}

func (r *recordingRunner) Run(_ context.Context, binary string, args []string, env []string, _ io.Writer, _ io.Writer) error {
	r.binary = binary
	r.args = append([]string(nil), args...)
	r.env = append([]string(nil), env...)
	if override := r.argAfter("-f", 2); override != "" {
		data, _ := os.ReadFile(override)
		r.override = string(data)
	}
	return nil
}

func (r *recordingRunner) argAfter(flag string, occurrence int) string {
	seen := 0
	for i := 0; i < len(r.args)-1; i++ {
		if r.args[i] == flag {
			seen++
			if seen == occurrence {
				return r.args[i+1]
			}
		}
	}
	return ""
}

func containsAll(values []string, wants ...string) bool {
	joined := "\x00" + strings.Join(values, "\x00") + "\x00"
	for _, want := range wants {
		if !strings.Contains(joined, "\x00"+want+"\x00") {
			return false
		}
	}
	return true
}

func emptyStreams() model.ExecIO {
	return model.ExecIO{}
}
