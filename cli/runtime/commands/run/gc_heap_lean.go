//go:build wago_lean

package run

import (
	"context"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
)

func gcFlags() []command.Flag { return nil }
func gcLongHelp() string      { return "" }

func gcConfiguration(*command.Ctx) (wago.GCConfig, bool, error) {
	return wago.GCConfig{}, false, nil
}

func instantiate(runtime *wago.Runtime, module *wago.Module, _ wago.GCConfig, _ bool) (*wago.Instance, error) {
	return runtime.Instantiate(context.Background(), module)
}
