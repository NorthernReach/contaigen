package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var envFileKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func parseEnvFiles(paths []string) ([]string, error) {
	out := []string(nil)
	for _, path := range paths {
		values, err := parseEnvFile(path)
		if err != nil {
			return nil, err
		}
		out = append(out, values...)
	}
	return out, nil
}

func parseEnvFile(path string) ([]string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("env file path is required")
	}
	absPath, err := filepath.Abs(expandHome(path))
	if err != nil {
		return nil, fmt.Errorf("resolve env file path: %w", err)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read env file %q: %w", absPath, err)
	}

	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimPrefix(text, "\ufeff")
	lines := strings.Split(text, "\n")
	values := make([]string, 0, len(lines))
	for index, raw := range lines {
		lineNo := index + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, rawValue, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("env file %q line %d must be KEY=VALUE", absPath, lineNo)
		}
		if !envFileKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("env file %q line %d has invalid key %q", absPath, lineNo, key)
		}
		value, err := parseEnvFileValue(strings.TrimSpace(rawValue), absPath, lineNo)
		if err != nil {
			return nil, err
		}
		values = append(values, key+"="+value)
	}
	return values, nil
}

func parseEnvFileValue(value string, path string, lineNo int) (string, error) {
	if value == "" {
		return "", nil
	}
	quote := value[0]
	if quote != '\'' && quote != '"' {
		return value, nil
	}
	if len(value) < 2 || value[len(value)-1] != quote {
		return "", fmt.Errorf("env file %q line %d has unterminated quoted value", path, lineNo)
	}
	if quote == '\'' {
		return value[1 : len(value)-1], nil
	}
	unquoted, err := strconv.Unquote(value)
	if err != nil {
		return "", fmt.Errorf("env file %q line %d has invalid quoted value: %w", path, lineNo, err)
	}
	return unquoted, nil
}
