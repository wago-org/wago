// Invocation routing selects plugin scope and runtime artifacts for a
// forwarded runtime command.
package plugin

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wago-org/wago/cli/internal/handoff"
	"github.com/wago-org/wago/internal/wagopaths"
)

type Environment interface {
	SelectScope(global, local, bare bool) error
	RuntimeBinary() (path string, configured bool, err error)
}

func Resolve(base string, profile wagopaths.Profile, args []string, environment Environment) (string, error) {
	if profile != wagopaths.ProfileStandard {
		return base, nil
	}
	if err := ApplyScope(args, environment); err != nil {
		return "", err
	}
	path, configured, err := environment.RuntimeBinary()
	if err != nil {
		return "", err
	}
	if configured {
		return path, nil
	}
	return base, nil
}

func ApplyScope(args []string, environment Environment) error {
	if len(args) == 0 {
		return nil
	}
	if args[0] == "plugin" || args[0] == "plugins" {
		if len(args) < 2 || (args[1] != "list" && args[1] != "ls" && args[1] != "inspect") {
			return nil
		}
		if err := environment.SelectScope(
			hasBoolFlag(args[2:], "--global", "-g"),
			hasBoolFlag(args[2:], "--local", "-l"),
			false,
		); err != nil {
			return fmt.Errorf("plugin %s: %w", args[1], err)
		}
		return nil
	}
	if args[0] == "build" {
		if err := environment.SelectScope(
			hasBoolFlag(args[1:], "--global", "-g"),
			hasBoolFlag(args[1:], "--local"),
			hasBoolFlag(args[1:], "--bare"),
		); err != nil {
			return fmt.Errorf("build: %w", err)
		}
		return nil
	}
	return applyRunScope(args, environment)
}

func applyRunScope(args []string, environment Environment) error {
	if len(args) == 0 {
		return nil
	}
	if args[0] == "run" {
		args = args[1:]
	} else if !handoff.LooksLikeRuntimeTarget(args[0]) && !strings.HasPrefix(args[0], "-") {
		return nil
	}
	global, local, bare := false, false, false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			continue
		}
		name, _, inline := strings.Cut(arg, "=")
		switch name {
		case "--global", "-g":
			global = true
		case "--local":
			local = true
		case "--bare":
			bare = true
		case "--invoke", "-e", "--core", "--watch-interval", "--gc-heap", "--gc-nursery":
			if !inline && index+1 < len(args) {
				index++
			}
		case "--parallel", "-p":
			if !inline && index+1 < len(args) {
				if _, err := strconv.Atoi(args[index+1]); err == nil {
					index++
				}
			}
		}
	}
	if err := environment.SelectScope(global, local, bare); err != nil {
		return fmt.Errorf("run: %w", err)
	}
	return nil
}

func hasBoolFlag(args []string, names ...string) bool {
	for _, arg := range args {
		for _, name := range names {
			if arg == name {
				return true
			}
		}
	}
	return false
}
