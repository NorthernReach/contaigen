package model

const (
	LabelManaged              = "io.contaigen.managed"
	LabelEnv                  = "io.contaigen.env"
	LabelVPN                  = "io.contaigen.vpn"
	LabelVPNConfig            = "io.contaigen.vpn.config"
	LabelVPNConfigMount       = "io.contaigen.vpn.config.mount"
	LabelVPNRouteMode         = "io.contaigen.vpn.route.mode"
	LabelVPNRoutes            = "io.contaigen.vpn.routes"
	LabelVPNNoVNCEnabled      = "io.contaigen.vpn.novnc.enabled"
	LabelVPNNoVNCPorts        = "io.contaigen.vpn.novnc.ports"
	LabelService              = "io.contaigen.service"
	LabelServiceAlias         = "io.contaigen.service.alias"
	LabelKind                 = "io.contaigen.kind"
	LabelProfile              = "io.contaigen.profile"
	LabelWorkspace            = "io.contaigen.workspace"
	LabelProject              = "io.contaigen.project"
	LabelVersion              = "io.contaigen.version"
	LabelShell                = "io.contaigen.shell"
	LabelUser                 = "io.contaigen.user"
	LabelWorkspaceMount       = "io.contaigen.workspace.mount"
	LabelNetworkProfile       = "io.contaigen.network.profile"
	LabelNetworkName          = "io.contaigen.network.name"
	LabelDesktopEnabled       = "io.contaigen.desktop.enabled"
	LabelDesktopProtocol      = "io.contaigen.desktop.protocol"
	LabelDesktopHostIP        = "io.contaigen.desktop.host_ip"
	LabelDesktopHostPort      = "io.contaigen.desktop.host_port"
	LabelDesktopContainerPort = "io.contaigen.desktop.container_port"
	LabelDesktopScheme        = "io.contaigen.desktop.scheme"
	LabelDesktopPath          = "io.contaigen.desktop.path"
	LabelDesktopUser          = "io.contaigen.desktop.user"
	LabelDesktopPasswordEnv   = "io.contaigen.desktop.password_env"
)

const (
	KindWorkbench = "workbench"
	KindNetwork   = "network"
	KindService   = "service"
	KindVPN       = "vpn"
)

func EnvironmentLabels(name string, shell string) map[string]string {
	return map[string]string{
		LabelManaged: "true",
		LabelEnv:     name,
		LabelKind:    KindWorkbench,
		LabelShell:   shell,
	}
}

func NetworkLabels(name string, profile string) map[string]string {
	return map[string]string{
		LabelManaged:        "true",
		LabelKind:           KindNetwork,
		LabelNetworkName:    name,
		LabelNetworkProfile: profile,
	}
}

func ServiceLabels(envName string, serviceName string) map[string]string {
	return map[string]string{
		LabelManaged: "true",
		LabelEnv:     envName,
		LabelKind:    KindService,
		LabelService: serviceName,
	}
}

func VPNLabels(name string, provider string) map[string]string {
	return map[string]string{
		LabelManaged:        "true",
		LabelKind:           KindVPN,
		LabelVPN:            name,
		LabelNetworkProfile: NetworkProfileVPN,
		LabelProfile:        provider,
	}
}
