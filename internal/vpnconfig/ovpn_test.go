package vpnconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOpenVPNRoutes(t *testing.T) {
	cfg, err := ParseOpenVPN([]byte(`client
dev tun
route 10.10.10.0 255.255.255.0
route 10.129.0.0 255.255.0.0
route-ipv6 fd00:1337::/64
redirect-gateway def1
`), "htb.ovpn")
	if err != nil {
		t.Fatalf("parse ovpn: %v", err)
	}
	got := FormatRoutes(cfg.Routes)
	want := "10.10.10.0/24, 10.129.0.0/16, fd00:1337::/64"
	if got != want {
		t.Fatalf("routes = %q, want %q", got, want)
	}
	if !cfg.HasRedirectGateway {
		t.Fatal("expected redirect-gateway detection")
	}
}

func TestPrepareSplitConfigDisablesDefaultRoute(t *testing.T) {
	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "htb.ovpn")
	if err := os.WriteFile(sourcePath, []byte(`client
dev tun
ca ca.crt
route 10.10.10.0 255.255.255.0
redirect-gateway def1
`), 0o600); err != nil {
		t.Fatalf("write ovpn: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "ca.crt"), []byte("cert"), 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	prepared, err := PrepareSplitConfig(sourcePath, filepath.Join(t.TempDir(), "vpn", "corp", "split"))
	if err != nil {
		t.Fatalf("prepare split config: %v", err)
	}
	data, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatalf("read prepared config: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"# contaigen managed OpenVPN config disabled: redirect-gateway def1",
		"route-nopull",
		"pull-filter ignore \"redirect-gateway\"",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prepared config missing %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(prepared.Path), "ca.crt")); err != nil {
		t.Fatalf("expected adjacent config files to be copied: %v", err)
	}
	if len(prepared.Routes) != 1 || prepared.Routes[0].CIDR != "10.10.10.0/24" {
		t.Fatalf("unexpected prepared routes: %#v", prepared.Routes)
	}
}

func TestPrepareSplitConfigDisablesUnsupportedDNSHooks(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "academy.ovpn")
	if err := os.WriteFile(sourcePath, []byte(`client
dev tun
script-security 2
up /etc/openvpn/update-resolv-conf
down /etc/openvpn/update-resolv-conf
`), 0o600); err != nil {
		t.Fatalf("write ovpn: %v", err)
	}
	prepared, err := PrepareSplitConfig(sourcePath, filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatalf("prepare split config: %v", err)
	}
	data, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatalf("read prepared config: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"# contaigen managed OpenVPN config disabled: up /etc/openvpn/update-resolv-conf",
		"# contaigen managed OpenVPN config disabled: down /etc/openvpn/update-resolv-conf",
		"script-security 2",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prepared config missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(strings.Join(prepared.Warnings, "\n"), "disabled update-resolv-conf hooks") {
		t.Fatalf("expected DNS hook warning, got %#v", prepared.Warnings)
	}
}

func TestPrepareSplitConfigAllowsServerPushedRoutes(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "academy.ovpn")
	if err := os.WriteFile(sourcePath, []byte(`client
dev tun
remote edge-us-academy-6.hackthebox.eu 1337
`), 0o600); err != nil {
		t.Fatalf("write ovpn: %v", err)
	}
	prepared, err := PrepareSplitConfig(sourcePath, filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatalf("prepare split config: %v", err)
	}
	data, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatalf("read prepared config: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "route-nopull") {
		t.Fatalf("server-pushed split config should not add route-nopull:\n%s", text)
	}
	if !strings.Contains(text, "pull-filter ignore \"redirect-gateway\"") {
		t.Fatalf("prepared config missing redirect-gateway pull-filter:\n%s", text)
	}
	if len(prepared.Routes) != 0 {
		t.Fatalf("expected no static routes, got %#v", prepared.Routes)
	}
	if !strings.Contains(strings.Join(prepared.Warnings, "\n"), "server-pushed routes") {
		t.Fatalf("expected server-pushed route warning, got %#v", prepared.Warnings)
	}
}
