//go:build amd64

package main

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func compileExplain(m *wasm.Module, guard bool, objectiveName string) (string, error) {
	objective, err := parseObjective(objectiveName)
	if err != nil {
		return "", err
	}
	var ms railshot.ModuleStats
	if _, err := railshot.CompileModuleWith(m, railshot.CompileOptions{
		ElideBoundsChecks: guard,
		Stats:             &ms,
		Objective:         &objective,
	}); err != nil {
		return "", err
	}
	return ms.String(), nil
}

func parseObjective(name string) (railshot.OptimizationObjective, error) {
	switch name {
	case "speed":
		return railshot.OptimizeSpeed, nil
	case "balanced":
		return railshot.OptimizeBalanced, nil
	case "size":
		return railshot.OptimizeSize, nil
	case "embedded":
		return railshot.OptimizeEmbedded, nil
	default:
		return 0, fmt.Errorf("unknown objective %q", name)
	}
}
