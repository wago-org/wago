package plugin

import "github.com/wago-org/wago/internal/wagopaths"

// sharedGlobalPluginDir is the one v1 global intent directory. V0 per-version
// manifests are deliberately not migrated or interpreted.
func sharedGlobalPluginDir(d wagopaths.Dirs) string { return d.Data }
