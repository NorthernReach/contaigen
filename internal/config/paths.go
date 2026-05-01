package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const appName = "contaigen"

type Paths struct {
	ConfigDir    string
	ConfigFile   string
	DataDir      string
	WorkspaceDir string
	TemplateDir  string
	BackupDir    string
	StateDir     string
	LogDir       string
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user home: %w", err)
	}
	if home == "" {
		return Paths{}, fmt.Errorf("resolve user home: empty path")
	}
	return pathsFor(runtime.GOOS, home, os.Getenv), nil
}

func pathsFor(goos string, home string, getenv func(string) string) Paths {
	configBase := configBaseDir(goos, home, getenv)
	dataBase := dataBaseDir(goos, home, getenv)
	stateBase := stateBaseDir(goos, home, getenv)

	paths := Paths{
		ConfigDir: filepath.Join(configBase, appName),
		DataDir:   filepath.Join(dataBase, appName),
		StateDir:  filepath.Join(stateBase, appName),
	}
	paths.ConfigFile = filepath.Join(paths.ConfigDir, "config.yaml")
	paths.WorkspaceDir = filepath.Join(paths.DataDir, "workspaces")
	paths.TemplateDir = filepath.Join(paths.DataDir, "templates")
	paths.BackupDir = filepath.Join(paths.DataDir, "backups")
	paths.LogDir = filepath.Join(paths.StateDir, "logs")
	return paths
}

func configBaseDir(goos string, home string, getenv func(string) string) string {
	switch goos {
	case "windows":
		if dir := getenv("APPDATA"); dir != "" {
			return dir
		}
		return filepath.Join(home, "AppData", "Roaming")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	default:
		if dir := getenv("XDG_CONFIG_HOME"); dir != "" {
			return dir
		}
		return filepath.Join(home, ".config")
	}
}

func dataBaseDir(goos string, home string, getenv func(string) string) string {
	switch goos {
	case "windows":
		if dir := getenv("LOCALAPPDATA"); dir != "" {
			return dir
		}
		return filepath.Join(home, "AppData", "Local")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	default:
		if dir := getenv("XDG_DATA_HOME"); dir != "" {
			return dir
		}
		return filepath.Join(home, ".local", "share")
	}
}

func stateBaseDir(goos string, home string, getenv func(string) string) string {
	switch goos {
	case "windows":
		if dir := getenv("LOCALAPPDATA"); dir != "" {
			return dir
		}
		return filepath.Join(home, "AppData", "Local")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	default:
		if dir := getenv("XDG_STATE_HOME"); dir != "" {
			return dir
		}
		return filepath.Join(home, ".local", "state")
	}
}
