// Package config owns persistent manager configuration such as shell completion.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Completion(shell string) (string, error) {
	commands := "run init add rm plugin auth module self build validate version status update cache config"
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "zsh":
		return "#compdef wago\n_arguments '1:command:(" + commands + ")' '*::args:->args'\n", nil
	case "bash":
		return "_wago_complete() { COMPREPLY=( $(compgen -W '" + commands + "' -- \"${COMP_WORDS[COMP_CWORD]}\") ); }\ncomplete -F _wago_complete wago\n", nil
	case "fish":
		var out strings.Builder
		for _, name := range strings.Fields(commands) {
			fmt.Fprintf(&out, "complete -c wago -f -n '__fish_use_subcommand' -a %s\n", name)
		}
		return out.String(), nil
	default:
		return "", fmt.Errorf("unsupported shell %q (want: zsh, bash, or fish)", shell)
	}
}

func InstallCompletion(shell, path, rc string) (string, error) {
	shell = strings.ToLower(strings.TrimSpace(shell))
	script, err := Completion(shell)
	if err != nil {
		return "", err
	}
	explicitPath := path != ""
	if path == "" {
		path, err = completionPath(shell)
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		return "", err
	}
	if explicitPath || shell == "fish" {
		return path, nil
	}
	if rc == "" {
		rc, err = shellRC(shell)
		if err != nil {
			return "", err
		}
	}
	return path, installHook(path, rc)
}

func completionPath(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch shell {
	case "zsh":
		return filepath.Join(home, ".wago", "completions", "wago.zsh"), nil
	case "bash":
		return filepath.Join(home, ".wago", "completions", "wago.bash"), nil
	case "fish":
		return filepath.Join(home, ".config", "fish", "completions", "wago.fish"), nil
	default:
		return "", fmt.Errorf("unsupported shell %q", shell)
	}
}

func shellRC(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc"), nil
	case "bash":
		return filepath.Join(home, ".bashrc"), nil
	default:
		return "", fmt.Errorf("unsupported shell %q", shell)
	}
}

func installHook(script, rc string) error {
	const marker = "# Wago completions"
	data, err := os.ReadFile(rc)
	if err == nil && strings.Contains(string(data), marker) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(rc), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(rc, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	prefix := ""
	if len(data) > 0 && data[len(data)-1] != '\n' {
		prefix = "\n"
	}
	quoted := strings.ReplaceAll(script, "'", "'\\''")
	_, err = fmt.Fprintf(file, "%s\n%s\n. '%s'\n", prefix, marker, quoted)
	return err
}
