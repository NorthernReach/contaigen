package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const (
	colorAuto   = "auto"
	colorAlways = "always"
	colorNever  = "never"
)

type palette struct {
	enabled bool
}

func colorFor(cmd *cobra.Command) palette {
	mode := colorAuto
	if flag := cmd.Root().PersistentFlags().Lookup("color"); flag != nil {
		mode = strings.ToLower(strings.TrimSpace(flag.Value.String()))
	}

	switch mode {
	case colorAlways:
		return palette{enabled: true}
	case colorNever:
		return palette{}
	default:
		if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
			return palette{}
		}
		if os.Getenv("CLICOLOR_FORCE") != "" {
			return palette{enabled: true}
		}
		return palette{enabled: isTerminal(cmd.OutOrStdout())}
	}
}

func validateColorMode(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case colorAuto, colorAlways, colorNever:
		return nil
	default:
		return fmt.Errorf("color must be auto, always, or never")
	}
}

func isTerminal(writer io.Writer) bool {
	// Keep auto color quiet for tests, pipes, and redirected output by only
	// enabling ANSI when Cobra is writing to an actual character device.
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (p palette) code(value string, code string) string {
	if !p.enabled || value == "" {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func (p palette) bold(value string) string   { return p.code(value, "1") }
func (p palette) cyan(value string) string   { return p.code(value, "36") }
func (p palette) green(value string) string  { return p.code(value, "32") }
func (p palette) yellow(value string) string { return p.code(value, "33") }
func (p palette) red(value string) string    { return p.code(value, "31") }

func (p palette) state(value string) string {
	switch strings.ToLower(value) {
	case "running":
		return p.green(value)
	case "created", "paused", "restarting":
		return p.yellow(value)
	case "exited", "dead", "removing":
		return p.red(value)
	default:
		return value
	}
}
