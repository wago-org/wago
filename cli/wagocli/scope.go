//go:build !wago_manager

package wagocli

import (
	"errors"
	"os"
)

const (
	pluginScopeGlobalEnv = "WAGO_GLOBAL"
	pluginScopeLocalEnv  = "WAGO_LOCAL"
	pluginScopeBareEnv   = "WAGO_BARE"
)

// scopeGlobal is the pure decision behind commands that mutate a plugin set.
// Mutations are local by default so `wago add` is reproducible and never changes
// user-wide state merely because the current directory lacks a manifest.
func scopeGlobal(explicitGlobal, explicitLocal bool) (bool, error) {
	switch {
	case explicitGlobal && explicitLocal:
		return false, errors.New("choose either --global or --local, not both")
	case explicitGlobal:
		return true, nil
	default:
		return false, nil
	}
}

// resolveScope validates explicit flags and returns whether to mutate the shared
// global plugin set.
func resolveScope(explicitGlobal, explicitLocal bool) bool {
	useGlobal, err := scopeGlobal(explicitGlobal, explicitLocal)
	if err != nil {
		fatal("pkg: %v", err)
	}
	return useGlobal
}

// selectPluginScope applies an explicit plugin scope to this invocation. With no
// explicit flag it leaves the inherited/default scope alone: local when a
// wago.json exists, otherwise global. The environment is the narrow seam shared
// by the original process and any plugin-aware binary it hands off to.
func selectPluginScope(global, local, bare bool) error {
	selected := 0
	for _, explicit := range []bool{global, local, bare} {
		if explicit {
			selected++
		}
	}
	if selected > 1 {
		return errors.New("choose only one of --local, --global, or --bare")
	}
	if selected == 0 {
		return nil
	}
	for _, name := range []string{pluginScopeGlobalEnv, pluginScopeLocalEnv, pluginScopeBareEnv} {
		if err := os.Unsetenv(name); err != nil {
			return err
		}
	}
	switch {
	case global:
		return os.Setenv(pluginScopeGlobalEnv, "1")
	case local:
		return os.Setenv(pluginScopeLocalEnv, "1")
	default:
		return os.Setenv(pluginScopeBareEnv, "1")
	}
}
