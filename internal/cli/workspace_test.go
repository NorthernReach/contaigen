package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NorthernReach/contaigen/internal/model"
)

func TestWorkspaceCreateCommand(t *testing.T) {
	manager := &fakeWorkspaceManager{}

	output, err := execute(t, NewRootCommand(Options{
		NewWorkspaceStore: fakeWorkspaceStore(manager),
	}), "workspace", "create", "client-a")
	if err != nil {
		t.Fatalf("workspace create failed: %v", err)
	}
	if !strings.Contains(output, "Created workspace client-a") {
		t.Fatalf("unexpected output:\n%s", output)
	}
	if len(manager.created) != 1 || manager.created[0].Name != "client-a" {
		t.Fatalf("unexpected create calls: %#v", manager.created)
	}
}

func TestWorkspaceListCommand(t *testing.T) {
	manager := &fakeWorkspaceManager{
		items: []model.Workspace{
			{Name: "client-a", Path: "/tmp/contaigen/workspaces/client-a"},
		},
	}

	output, err := execute(t, NewRootCommand(Options{
		NewWorkspaceStore: fakeWorkspaceStore(manager),
	}), "workspace", "list")
	if err != nil {
		t.Fatalf("workspace list failed: %v", err)
	}
	for _, want := range []string{"NAME", "client-a", "/tmp/contaigen/workspaces/client-a"} {
		if !strings.Contains(output, want) {
			t.Fatalf("list output missing %q:\n%s", want, output)
		}
	}
}

func TestWorkspaceBackupCommand(t *testing.T) {
	manager := &fakeWorkspaceManager{}

	output, err := execute(t, NewRootCommand(Options{
		NewWorkspaceStore: fakeWorkspaceStore(manager),
	}), "workspace", "backup", "client-a", "--output", "/tmp/client-a.tar.gz")
	if err != nil {
		t.Fatalf("workspace backup failed: %v", err)
	}
	if !strings.Contains(output, "Backed up workspace client-a") || !strings.Contains(output, "/tmp/client-a.tar.gz") {
		t.Fatalf("unexpected output:\n%s", output)
	}
	if len(manager.backups) != 1 || manager.backups[0].OutputPath != "/tmp/client-a.tar.gz" {
		t.Fatalf("unexpected backup calls: %#v", manager.backups)
	}
}

func TestWorkspaceBackupCommandReadsPasswordFile(t *testing.T) {
	manager := &fakeWorkspaceManager{}
	passwordPath := filepath.Join(t.TempDir(), "backup.pass")
	if err := os.WriteFile(passwordPath, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	output, err := execute(t, NewRootCommand(Options{
		NewWorkspaceStore: fakeWorkspaceStore(manager),
	}), "workspace", "backup", "client-a", "--password-file", passwordPath)
	if err != nil {
		t.Fatalf("workspace encrypted backup failed: %v", err)
	}
	if !strings.Contains(output, "Encrypted: yes") {
		t.Fatalf("expected encrypted backup output:\n%s", output)
	}
	if len(manager.backups) != 1 || manager.backups[0].Password != "secret" {
		t.Fatalf("unexpected backup request: %#v", manager.backups)
	}
}

func TestWorkspaceRestoreCommand(t *testing.T) {
	manager := &fakeWorkspaceManager{}

	output, err := execute(t, NewRootCommand(Options{
		NewWorkspaceStore: fakeWorkspaceStore(manager),
	}), "workspace", "restore", "/tmp/client-a.tar.gz", "--name", "client-b", "--path", "/tmp/client-b")
	if err != nil {
		t.Fatalf("workspace restore failed: %v", err)
	}
	for _, want := range []string{"Restored workspace client-b", "Path: /tmp/client-b", "Source: /tmp/client-a.tar.gz", "Files: 2"} {
		if !strings.Contains(output, want) {
			t.Fatalf("restore output missing %q:\n%s", want, output)
		}
	}
	if len(manager.restores) != 1 {
		t.Fatalf("expected one restore call, got %d", len(manager.restores))
	}
	req := manager.restores[0]
	if req.Name != "client-b" || req.InputPath != "/tmp/client-a.tar.gz" || req.Path != "/tmp/client-b" {
		t.Fatalf("unexpected restore request: %#v", req)
	}
}

func TestWorkspaceRestoreCommandReadsPassword(t *testing.T) {
	manager := &fakeWorkspaceManager{}

	_, err := execute(t, NewRootCommand(Options{
		NewWorkspaceStore: fakeWorkspaceStore(manager),
	}), "workspace", "restore", "/tmp/client-a.tar.gz.c3enc", "--name", "client-b", "--password", "secret")
	if err != nil {
		t.Fatalf("workspace encrypted restore failed: %v", err)
	}
	if len(manager.restores) != 1 || manager.restores[0].Password != "secret" {
		t.Fatalf("unexpected restore request: %#v", manager.restores)
	}
}
