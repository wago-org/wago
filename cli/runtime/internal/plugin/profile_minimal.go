//go:build wago_minimal

package plugin

import (
	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
)

func Flags() []command.Flag { return nil }

func LoadRuntime(cfg *wago.RuntimeConfig, _ string) *wago.Runtime {
	return wago.NewRuntime(wago.WithRuntimeConfig(cfg))
}

func Summary() string { return compiledPluginSummary() }

func ScopeLabel() string { return "bare" }
