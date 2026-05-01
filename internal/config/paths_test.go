package config

import (
	"path/filepath"
	"testing"
)

func TestPathsForLinuxDefaults(t *testing.T) {
	paths := pathsFor("linux", "/home/alice", emptyEnv)

	assertPath(t, paths.ConfigFile, filepath.Join("/home/alice", ".config", "contaigen", "config.yaml"))
	assertPath(t, paths.DataDir, filepath.Join("/home/alice", ".local", "share", "contaigen"))
	assertPath(t, paths.WorkspaceDir, filepath.Join("/home/alice", ".local", "share", "contaigen", "workspaces"))
	assertPath(t, paths.TemplateDir, filepath.Join("/home/alice", ".local", "share", "contaigen", "templates"))
	assertPath(t, paths.BackupDir, filepath.Join("/home/alice", ".local", "share", "contaigen", "backups"))
	assertPath(t, paths.LogDir, filepath.Join("/home/alice", ".local", "state", "contaigen", "logs"))
}

func TestPathsForLinuxXDGOverrides(t *testing.T) {
	env := map[string]string{
		"XDG_CONFIG_HOME": "/tmp/config",
		"XDG_DATA_HOME":   "/tmp/data",
		"XDG_STATE_HOME":  "/tmp/state",
	}

	paths := pathsFor("linux", "/home/alice", func(key string) string {
		return env[key]
	})

	assertPath(t, paths.ConfigDir, filepath.Join("/tmp/config", "contaigen"))
	assertPath(t, paths.DataDir, filepath.Join("/tmp/data", "contaigen"))
	assertPath(t, paths.StateDir, filepath.Join("/tmp/state", "contaigen"))
}

func TestPathsForDarwin(t *testing.T) {
	paths := pathsFor("darwin", "/Users/alice", emptyEnv)

	base := filepath.Join("/Users/alice", "Library", "Application Support", "contaigen")
	assertPath(t, paths.ConfigDir, base)
	assertPath(t, paths.DataDir, base)
	assertPath(t, paths.StateDir, base)
	assertPath(t, paths.ConfigFile, filepath.Join(base, "config.yaml"))
}

func emptyEnv(string) string {
	return ""
}

func assertPath(t *testing.T, got string, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("path mismatch\ngot:  %s\nwant: %s", got, want)
	}
}
