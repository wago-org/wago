// Package automation defines process-wide CLI policy for deterministic,
// non-interactive and machine-readable operation. The manager forwards these
// settings through the environment when it launches a runtime.
package automation

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

const (
	EnvNonInteractive = "WAGO_NONINTERACTIVE"
	EnvJSON           = "WAGO_JSON"
	EnvDryRun         = "WAGO_DRY_RUN"
	EnvLocked         = "WAGO_LOCKED"
	EnvOffline        = "WAGO_OFFLINE"
)

type Options struct {
	JSON, NoInput, DryRun, Locked, Offline bool
}

var state struct {
	sync.RWMutex
	options Options
}

func FromEnv() Options {
	return Options{
		JSON:    envTrue(EnvJSON),
		NoInput: envTrue(EnvNonInteractive),
		DryRun:  envTrue(EnvDryRun),
		Locked:  envTrue(EnvLocked),
		Offline: envTrue(EnvOffline),
	}
}

func Configure(options Options) {
	state.Lock()
	state.options = options
	state.Unlock()
}

func Current() Options {
	state.RLock()
	options := state.options
	state.RUnlock()
	return options
}

func Merge(options Options) Options {
	current := Current()
	current.JSON = current.JSON || options.JSON
	current.NoInput = current.NoInput || options.NoInput
	current.DryRun = current.DryRun || options.DryRun
	current.Locked = current.Locked || options.Locked
	current.Offline = current.Offline || options.Offline
	return current
}

func JSON() bool    { return Current().JSON || envTrue(EnvJSON) }
func NoInput() bool { return Current().NoInput || envTrue(EnvNonInteractive) }
func DryRun() bool  { return Current().DryRun || envTrue(EnvDryRun) }
func Locked() bool  { return Current().Locked || envTrue(EnvLocked) }
func Offline() bool { return Current().Offline || envTrue(EnvOffline) }

func Reset() { Configure(Options{}) }

// ParseLeading consumes automation flags placed before the command name.
func ParseLeading(args []string) ([]string, error) {
	options := Merge(FromEnv())
	index := 0
	for index < len(args) {
		switch args[index] {
		case "--json", "-j":
			options.JSON = true
		case "--no-input":
			options.NoInput = true
		case "--dry-run":
			options.DryRun = true
		case "--locked":
			options.Locked = true
		case "--offline":
			options.Offline = true
		default:
			Configure(options)
			return args[index:], nil
		}
		index++
	}
	Configure(options)
	return args[index:], nil
}

// Environment returns env with policy variables replaced by the current state.
func Environment(env []string) []string {
	options := Merge(FromEnv())
	values := map[string]bool{
		EnvJSON: options.JSON, EnvNonInteractive: options.NoInput,
		EnvDryRun: options.DryRun, EnvLocked: options.Locked, EnvOffline: options.Offline,
	}
	out := make([]string, 0, len(env)+len(values))
	for _, entry := range env {
		name := entry
		if equals := strings.IndexByte(entry, '='); equals >= 0 {
			name = entry[:equals]
		}
		if _, managed := values[name]; !managed {
			out = append(out, entry)
		}
	}
	for name, enabled := range values {
		if enabled {
			out = append(out, name+"=1")
		}
	}
	return out
}

// ConfigureCommand applies offline constraints to child tools while preserving
// all other environment entries. GOPROXY=off still permits Go's local caches.
func ConfigureCommand(command *exec.Cmd) {
	if !Offline() {
		return
	}
	env := command.Env
	if env == nil {
		env = os.Environ()
	}
	filtered := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, "GOPROXY=") {
			filtered = append(filtered, entry)
		}
	}
	command.Env = append(filtered, "GOPROXY=off")
}

func RequireOnline(action string) error {
	if Offline() {
		return fmt.Errorf("offline mode prevents %s", action)
	}
	return nil
}

// PrintPlan emits one deterministic mutation plan and no side effects.
func PrintPlan(action string, details map[string]any) {
	if JSON() {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"dryRun": true, "action": action, "details": details,
		})
		return
	}
	fmt.Printf("Dry run: %s\n", action)
	keys := make([]string, 0, len(details))
	for key := range details {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Printf("  %-12s %v\n", key, details[key])
	}
}

func envTrue(name string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	return value != "" && value != "0" && value != "false" && value != "no"
}
