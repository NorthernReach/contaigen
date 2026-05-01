package workspace

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NorthernReach/contaigen/internal/model"
)

func TestCreateInspectAndListWorkspace(t *testing.T) {
	store := testStore(t)

	ws, err := store.Create(context.Background(), model.CreateWorkspaceRequest{Name: "client-a"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if ws.Name != "client-a" {
		t.Fatalf("unexpected workspace name: %q", ws.Name)
	}
	if _, err := os.Stat(filepath.Join(ws.Path, metadataFile)); err != nil {
		t.Fatalf("expected metadata file: %v", err)
	}

	inspected, err := store.Inspect(context.Background(), "client-a")
	if err != nil {
		t.Fatalf("inspect workspace: %v", err)
	}
	if inspected.Path != ws.Path {
		t.Fatalf("inspect path mismatch: %s != %s", inspected.Path, ws.Path)
	}

	items, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	if len(items) != 1 || items[0].Name != "client-a" {
		t.Fatalf("unexpected workspace list: %#v", items)
	}
}

func TestEnsureReturnsExistingWorkspace(t *testing.T) {
	store := testStore(t)

	created, err := store.Ensure(context.Background(), model.EnsureWorkspaceRequest{Name: "client-a"})
	if err != nil {
		t.Fatalf("ensure workspace: %v", err)
	}
	ensured, err := store.Ensure(context.Background(), model.EnsureWorkspaceRequest{Name: "client-a"})
	if err != nil {
		t.Fatalf("ensure existing workspace: %v", err)
	}
	if ensured.Path != created.Path {
		t.Fatalf("expected existing workspace path, got %s", ensured.Path)
	}
}

func TestBackupWorkspace(t *testing.T) {
	store := testStore(t)

	ws, err := store.Create(context.Background(), model.CreateWorkspaceRequest{Name: "client-a"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws.Path, "notes.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	backup, err := store.Backup(context.Background(), model.BackupWorkspaceRequest{Name: "client-a"})
	if err != nil {
		t.Fatalf("backup workspace: %v", err)
	}
	if backup.SizeBytes == 0 {
		t.Fatal("expected non-empty backup")
	}
	assertTarContains(t, backup.Path, "client-a/notes.txt")
}

func TestEncryptedBackupWorkspace(t *testing.T) {
	store := testStore(t)

	ws, err := store.Create(context.Background(), model.CreateWorkspaceRequest{Name: "client-a"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws.Path, "secret.txt"), []byte("customer data"), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	backup, err := store.Backup(context.Background(), model.BackupWorkspaceRequest{
		Name:     "client-a",
		Password: "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("backup encrypted workspace: %v", err)
	}
	if !backup.Encrypted || !strings.HasSuffix(backup.Path, ".tar.gz.c3enc") {
		t.Fatalf("expected encrypted backup path/result: %#v", backup)
	}
	assertEncryptedBackupMagic(t, backup.Path)

	restore, err := store.Restore(context.Background(), model.RestoreWorkspaceRequest{
		Name:      "client-b",
		InputPath: backup.Path,
		Password:  "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("restore encrypted workspace: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(restore.Workspace.Path, "secret.txt"))
	if err != nil {
		t.Fatalf("read restored encrypted backup file: %v", err)
	}
	if string(data) != "customer data" {
		t.Fatalf("unexpected restored encrypted backup content: %q", string(data))
	}
}

func TestEncryptedBackupRestoreRequiresPassword(t *testing.T) {
	store := testStore(t)

	ws, err := store.Create(context.Background(), model.CreateWorkspaceRequest{Name: "client-a"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws.Path, "secret.txt"), []byte("customer data"), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}
	backup, err := store.Backup(context.Background(), model.BackupWorkspaceRequest{
		Name:     "client-a",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("backup encrypted workspace: %v", err)
	}

	_, err = store.Restore(context.Background(), model.RestoreWorkspaceRequest{
		Name:      "client-b",
		InputPath: backup.Path,
	})
	if err == nil {
		t.Fatal("expected encrypted restore without password to fail")
	}
	if !strings.Contains(err.Error(), "requires a password") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRestoreWorkspace(t *testing.T) {
	store := testStore(t)

	ws, err := store.Create(context.Background(), model.CreateWorkspaceRequest{Name: "client-a"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(ws.Path, "findings"), 0o750); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws.Path, "findings", "notes.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}
	backup, err := store.Backup(context.Background(), model.BackupWorkspaceRequest{Name: "client-a"})
	if err != nil {
		t.Fatalf("backup workspace: %v", err)
	}

	restore, err := store.Restore(context.Background(), model.RestoreWorkspaceRequest{
		Name:      "client-b",
		InputPath: backup.Path,
	})
	if err != nil {
		t.Fatalf("restore workspace: %v", err)
	}
	if restore.Workspace.Name != "client-b" || restore.Files != 1 || restore.SizeBytes != 5 {
		t.Fatalf("unexpected restore result: %#v", restore)
	}
	data, err := os.ReadFile(filepath.Join(restore.Workspace.Path, "findings", "notes.txt"))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected restored file content: %q", string(data))
	}
	inspected, err := store.Inspect(context.Background(), "client-b")
	if err != nil {
		t.Fatalf("inspect restored workspace: %v", err)
	}
	if inspected.Name != "client-b" {
		t.Fatalf("restore should rewrite metadata for new workspace name: %#v", inspected)
	}
}

func TestRestoreWorkspaceRejectsNonEmptyTarget(t *testing.T) {
	store := testStore(t)

	ws, err := store.Create(context.Background(), model.CreateWorkspaceRequest{Name: "client-a"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws.Path, "notes.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}
	backup, err := store.Backup(context.Background(), model.BackupWorkspaceRequest{Name: "client-a"})
	if err != nil {
		t.Fatalf("backup workspace: %v", err)
	}
	target := filepath.Join(store.root, "client-b")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatalf("create target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "existing.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatalf("write target file: %v", err)
	}

	_, err = store.Restore(context.Background(), model.RestoreWorkspaceRequest{
		Name:      "client-b",
		InputPath: backup.Path,
	})
	if err == nil {
		t.Fatal("expected non-empty restore target to fail")
	}
	if !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRestoreWorkspaceRejectsUnsafeArchivePath(t *testing.T) {
	store := testStore(t)
	archivePath := filepath.Join(t.TempDir(), "unsafe.tar.gz")
	writeTestArchive(t, archivePath, "client-a/../evil.txt", "bad")

	_, err := store.Restore(context.Background(), model.RestoreWorkspaceRequest{
		Name:      "client-b",
		InputPath: archivePath,
	})
	if err == nil {
		t.Fatal("expected unsafe archive path to fail")
	}
	if !strings.Contains(err.Error(), "unsafe archive path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRestoreWorkspaceRejectsSymlink(t *testing.T) {
	store := testStore(t)
	archivePath := filepath.Join(t.TempDir(), "symlink.tar.gz")
	writeTestSymlinkArchive(t, archivePath, "client-a/link", "/tmp/outside")

	_, err := store.Restore(context.Background(), model.RestoreWorkspaceRequest{
		Name:      "client-b",
		InputPath: archivePath,
	})
	if err == nil {
		t.Fatal("expected symlink archive entry to fail")
	}
	if !strings.Contains(err.Error(), "unsupported symbolic link") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveWorkspaceDeletesWorkspaceDirectory(t *testing.T) {
	store := testStore(t)
	ws, err := store.Create(context.Background(), model.CreateWorkspaceRequest{Name: "client-a"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws.Path, "notes.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	removed, err := store.Remove(context.Background(), model.RemoveWorkspaceRequest{Name: "client-a"})
	if err != nil {
		t.Fatalf("remove workspace: %v", err)
	}
	if removed.Workspace.Name != "client-a" {
		t.Fatalf("unexpected removed workspace: %#v", removed)
	}
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("expected workspace path to be removed, stat err=%v", err)
	}
}

func TestRemoveWorkspaceRejectsCustomPathWithoutMetadata(t *testing.T) {
	store := testStore(t)
	path := filepath.Join(t.TempDir(), "custom")
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatalf("create custom path: %v", err)
	}

	_, err := store.Remove(context.Background(), model.RemoveWorkspaceRequest{
		Name: "client-a",
		Path: path,
	})
	if err == nil {
		t.Fatal("expected custom path without metadata to be rejected")
	}
	if !strings.Contains(err.Error(), "without Contaigen metadata") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("custom path should remain: %v", err)
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()

	root := filepath.Join(t.TempDir(), "workspaces")
	backupDir := filepath.Join(t.TempDir(), "backups")
	store := NewStore(root, backupDir)
	store.now = func() time.Time {
		return time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	}
	return store
}

func assertEncryptedBackupMagic(t *testing.T, path string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read encrypted backup: %v", err)
	}
	if !strings.HasPrefix(string(data), string(encryptedBackupMagic)) {
		t.Fatalf("encrypted backup missing magic header")
	}
	if strings.Contains(string(data), "customer data") {
		t.Fatalf("encrypted backup contains plaintext payload")
	}
}

func writeTestSymlinkArchive(t *testing.T, path string, name string, target string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test archive: %v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeSymlink,
		Linkname: target,
		Mode:     0o777,
	}); err != nil {
		t.Fatalf("write test symlink header: %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close test tar writer: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close test gzip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close test archive: %v", err)
	}
}

func writeTestArchive(t *testing.T, path string, name string, body string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test archive: %v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o600,
		Size: int64(len(body)),
	}); err != nil {
		t.Fatalf("write test archive header: %v", err)
	}
	if _, err := tarWriter.Write([]byte(body)); err != nil {
		t.Fatalf("write test archive body: %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close test tar writer: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close test gzip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close test archive: %v", err)
	}
}

func assertTarContains(t *testing.T, path string, want string) {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("open gzip: %v", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			break
		}
		if header.Name == want {
			return
		}
	}
	t.Fatalf("backup %s did not contain %s", path, want)
}
