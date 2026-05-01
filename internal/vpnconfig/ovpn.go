package vpnconfig

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
)

type Route struct {
	CIDR      string
	Directive string
	Line      int
}

type OpenVPNConfig struct {
	Path                    string
	Routes                  []Route
	HasRedirectGateway      bool
	HasRouteNoPull          bool
	HasUpdateResolvConfHook bool
}

type PreparedConfig struct {
	SourcePath string
	Path       string
	Routes     []Route
	Warnings   []string
}

func ParseOpenVPNFile(path string) (OpenVPNConfig, error) {
	data, abs, err := readFile(path)
	if err != nil {
		return OpenVPNConfig{}, err
	}
	return ParseOpenVPN(data, abs)
}

func ParseOpenVPN(data []byte, path string) (OpenVPNConfig, error) {
	cfg := OpenVPNConfig{Path: path}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		fields := directiveFields(line)
		if len(fields) == 0 {
			continue
		}
		directive := strings.TrimPrefix(strings.ToLower(fields[0]), "--")
		switch directive {
		case "route":
			route, err := parseIPv4Route(fields, lineNo)
			if err != nil {
				return OpenVPNConfig{}, err
			}
			cfg.Routes = append(cfg.Routes, route)
		case "route-ipv6":
			route, err := parseIPv6Route(fields, lineNo)
			if err != nil {
				return OpenVPNConfig{}, err
			}
			cfg.Routes = append(cfg.Routes, route)
		case "redirect-gateway":
			cfg.HasRedirectGateway = true
		case "route-nopull":
			cfg.HasRouteNoPull = true
		case "up", "down":
			if hasUpdateResolvConfHook(fields) {
				cfg.HasUpdateResolvConfHook = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return OpenVPNConfig{}, err
	}
	return cfg, nil
}

func PrepareSplitConfig(sourcePath string, targetDir string) (PreparedConfig, error) {
	data, abs, err := readFile(sourcePath)
	if err != nil {
		return PreparedConfig{}, err
	}
	cfg, err := ParseOpenVPN(data, abs)
	if err != nil {
		return PreparedConfig{}, err
	}
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return PreparedConfig{}, err
	}
	if err := copyConfigDirectory(filepath.Dir(abs), targetDir); err != nil {
		return PreparedConfig{}, err
	}

	// Split mode always writes a managed copy. The source .ovpn and any sibling
	// cert/key files stay untouched, while relative references keep working from
	// the copied config directory mounted into the VPN container.
	patched := patchSplitConfig(data, cfg)
	targetPath := filepath.Join(targetDir, filepath.Base(abs))
	if err := os.WriteFile(targetPath, patched, 0o600); err != nil {
		return PreparedConfig{}, err
	}

	warnings := splitWarnings(cfg)
	if cfg.HasRedirectGateway {
		warnings = append(warnings, "split route mode disabled redirect-gateway from the OpenVPN config")
	}
	if cfg.HasUpdateResolvConfHook {
		warnings = append(warnings, "managed OpenVPN config disabled update-resolv-conf hooks because the default VPN sidecar image does not include that host DNS script")
	}
	return PreparedConfig{
		SourcePath: abs,
		Path:       targetPath,
		Routes:     cfg.Routes,
		Warnings:   warnings,
	}, nil
}

func FormatRoutes(routes []Route) string {
	if len(routes) == 0 {
		return "none"
	}
	values := make([]string, 0, len(routes))
	for _, route := range routes {
		values = append(values, route.CIDR)
	}
	return strings.Join(values, ", ")
}

func splitWarnings(cfg OpenVPNConfig) []string {
	if len(cfg.Routes) > 0 {
		return []string{fmt.Sprintf("split route mode discovered VPN routes: %s", FormatRoutes(cfg.Routes))}
	}
	warnings := []string{"split route mode found no static route directives; it will accept server-pushed routes while blocking default-gateway pushes"}
	if cfg.HasRouteNoPull {
		warnings = append(warnings, "split route mode found route-nopull in the OpenVPN config; server-pushed routes may be blocked by the config")
	}
	return warnings
}

func readFile(path string) ([]byte, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", err
	}
	return data, abs, nil
}

func directiveFields(line string) []string {
	line = stripInlineComment(line)
	return strings.Fields(line)
}

func stripInlineComment(line string) string {
	inSingle := false
	inDouble := false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#', ';':
			if !inSingle && !inDouble && (i == 0 || isSpace(rune(line[i-1]))) {
				return strings.TrimSpace(line[:i])
			}
		}
	}
	return strings.TrimSpace(line)
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t'
}

func hasUpdateResolvConfHook(fields []string) bool {
	if len(fields) < 2 {
		return false
	}
	for _, field := range fields[1:] {
		if strings.Contains(field, "update-resolv-conf") {
			return true
		}
	}
	return false
}

func parseIPv4Route(fields []string, line int) (Route, error) {
	if len(fields) < 2 {
		return Route{}, fmt.Errorf("line %d: route requires a network", line)
	}
	if strings.Contains(fields[1], "/") {
		prefix, err := netip.ParsePrefix(fields[1])
		if err != nil {
			return Route{}, fmt.Errorf("line %d: parse route %q: %w", line, fields[1], err)
		}
		return Route{CIDR: prefix.String(), Directive: "route", Line: line}, nil
	}

	ip, err := netip.ParseAddr(fields[1])
	if err != nil || !ip.Is4() {
		return Route{}, fmt.Errorf("line %d: route network %q must be IPv4", line, fields[1])
	}

	prefixLen := 32
	if len(fields) >= 3 && fields[2] != "" && fields[2] != "default" {
		mask := net.ParseIP(fields[2]).To4()
		if mask == nil {
			return Route{}, fmt.Errorf("line %d: route netmask %q must be IPv4", line, fields[2])
		}
		ones, bits := net.IPMask(mask).Size()
		if bits != 32 {
			return Route{}, fmt.Errorf("line %d: route netmask %q is not contiguous", line, fields[2])
		}
		prefixLen = ones
	}

	prefix := netip.PrefixFrom(ip, prefixLen).Masked()
	return Route{CIDR: prefix.String(), Directive: "route", Line: line}, nil
}

func parseIPv6Route(fields []string, line int) (Route, error) {
	if len(fields) < 2 {
		return Route{}, fmt.Errorf("line %d: route-ipv6 requires a network", line)
	}
	prefix, err := netip.ParsePrefix(fields[1])
	if err != nil {
		return Route{}, fmt.Errorf("line %d: parse route-ipv6 %q: %w", line, fields[1], err)
	}
	return Route{CIDR: prefix.String(), Directive: "route-ipv6", Line: line}, nil
}

func patchSplitConfig(data []byte, cfg OpenVPNConfig) []byte {
	var out bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		raw := scanner.Text()
		fields := directiveFields(strings.TrimSpace(raw))
		if len(fields) > 0 && shouldDisableDirectiveInManagedConfig(fields) {
			out.WriteString("# contaigen managed OpenVPN config disabled: ")
			out.WriteString(raw)
			out.WriteByte('\n')
			continue
		}
		out.WriteString(raw)
		out.WriteByte('\n')
	}

	out.WriteString("\n# contaigen split route mode\n")
	// Static routes in the .ovpn are preserved above, then route-nopull prevents
	// the server from also pushing a default route through the VPN. When no static
	// routes are present, we allow server-pushed routes so providers like HTB can
	// still announce lab networks dynamically while redirect-gateway is filtered.
	if len(cfg.Routes) > 0 && !cfg.HasRouteNoPull {
		out.WriteString("route-nopull\n")
	}
	out.WriteString("pull-filter ignore \"redirect-gateway\"\n")
	return out.Bytes()
}

func shouldDisableDirectiveInManagedConfig(fields []string) bool {
	// The sidecar should not inherit host-oriented DNS hooks or full-tunnel
	// redirect directives from a user config. Docker provides the container DNS
	// environment, and split mode owns the default-gateway decision explicitly.
	directive := strings.TrimPrefix(strings.ToLower(fields[0]), "--")
	return directive == "redirect-gateway" || ((directive == "up" || directive == "down") && hasUpdateResolvConfHook(fields))
}

func copyConfigDirectory(sourceDir string, targetDir string) error {
	sourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return err
	}
	targetDir, err = filepath.Abs(targetDir)
	if err != nil {
		return err
	}
	if sourceDir == targetDir {
		return nil
	}
	return filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(targetDir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}

func copyFile(source string, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
