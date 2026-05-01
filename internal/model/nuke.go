package model

type NukeRequest struct {
	BackupWorkspaces bool
	BackupPassword   string
	StopTimeout      int
}

type NukePlan struct {
	Environments []Environment
	Services     []Service
	VPNGateways  []VPNGateway
	Networks     []Network
	Workspaces   []Workspace
}

type NukeResult struct {
	Plan              NukePlan
	WorkspaceBackups  []WorkspaceBackup
	RemovedWorkspaces []WorkspaceRemove
	Errors            []NukeError
}

type NukeError struct {
	ResourceType string
	Name         string
	Action       string
	Message      string
}
