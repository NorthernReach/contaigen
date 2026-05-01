package model

import "time"

const DefaultWorkspaceMountPath = "/workspace"

type Workspace struct {
	Name      string
	Path      string
	CreatedAt time.Time
}

type CreateWorkspaceRequest struct {
	Name string
	Path string
}

type EnsureWorkspaceRequest struct {
	Name string
	Path string
}

type RemoveWorkspaceRequest struct {
	Name string
	Path string
}

type BackupWorkspaceRequest struct {
	Name       string
	Path       string
	OutputPath string
	Password   string
}

type WorkspaceBackup struct {
	Workspace Workspace
	Path      string
	SizeBytes int64
	Encrypted bool
}

type RestoreWorkspaceRequest struct {
	Name      string
	InputPath string
	Path      string
	Password  string
}

type WorkspaceRestore struct {
	Workspace Workspace
	Path      string
	Files     int
	SizeBytes int64
}

type WorkspaceRemove struct {
	Workspace Workspace
}
