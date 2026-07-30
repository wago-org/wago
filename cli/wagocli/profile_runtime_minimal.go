//go:build !wago_manager && wago_minimal

package wagocli

import "github.com/wago-org/wago"

func prepareRunnerInvocation([]string) {}
func runProfileFlags() []Flag          { return nil }
func prepareRunPlugins()               {}

func loadRunRuntime(cfg *wago.RuntimeConfig, _ string) *wago.Runtime {
	return wago.NewRuntime(wago.WithRuntimeConfig(cfg))
}

func versionPluginSummary() string { return compiledPluginSummary() }
