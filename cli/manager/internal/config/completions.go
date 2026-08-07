// Package config owns persistent manager configuration such as shell completion.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Completion(shell string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "zsh":
		return "#compdef wago\n" +
			"autoload -Uz compinit\n" +
			"(( $+functions[compdef] )) || compinit\n" +
			"_wago() {\n" +
			"  local -a candidates\n" +
			"  candidates=(\"${(@f)$(command wago __complete \"${(@)words[2,-1]}\")}\")\n" +
			"  if (( ${#candidates} )); then\n" +
			"    compadd -- \"${candidates[@]}\"\n" +
			"  else\n" +
			"    _files\n" +
			"  fi\n" +
			"}\n" +
			"compdef _wago wago\n", nil
	case "bash":
		return "_wago_complete() {\n" +
			"  local candidate\n" +
			"  COMPREPLY=()\n" +
			"  while IFS= read -r candidate; do\n" +
			"    COMPREPLY+=(\"$candidate\")\n" +
			"  done < <(command wago __complete \"${COMP_WORDS[@]:1}\")\n" +
			"  if (( ${#COMPREPLY[@]} == 0 )); then\n" +
			"    compopt -o default 2>/dev/null || true\n" +
			"  fi\n" +
			"}\n" +
			"complete -o bashdefault -o default -F _wago_complete wago\n", nil
	case "fish":
		return "function __wago_complete\n" +
			"  set -l words (commandline -opc)\n" +
			"  set -e words[1]\n" +
			"  set -l current (commandline -ct)\n" +
			"  set -l candidates (command wago __complete $words \"$current\")\n" +
			"  if test (count $candidates) -gt 0\n" +
			"    printf '%s\\n' $candidates\n" +
			"  else\n" +
			"    __fish_complete_path \"$current\"\n" +
			"  end\n" +
			"end\n" +
			"complete -c wago -f -a '(__wago_complete)'\n", nil
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
