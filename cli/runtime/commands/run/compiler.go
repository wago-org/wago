package run

import (
	"fmt"

	"github.com/wago-org/wago/cli/internal/command"
)

// BackendFlags exposes canonical compiler-backend selection and convenience aliases.
func BackendFlags() []command.Flag {
	return []command.Flag{
		{Name: "backend", Arg: "<name>", Help: "compiler backend: railshot | dragline (default railshot)"},
		{Name: "compiler-fallback", Arg: "<name>", Help: "whole-module fallback: none | railshot (default none)"},
		{Name: "railshot", Bool: true, Help: "use the Railshot compiler"},
		{Name: "dragline", Bool: true, Help: "use the Dragline compiler (strict; no fallback)"},
	}
}

// BackendOverride resolves canonical and convenience backend flags.
func BackendOverride(ctx *command.Ctx) (string, error) {
	selected := ctx.Str("backend")
	railshot, dragline := ctx.Bool("railshot"), ctx.Bool("dragline")
	if railshot && dragline {
		return "", fmt.Errorf("conflicting --railshot and --dragline")
	}
	alias := ""
	if railshot {
		alias = "railshot"
	} else if dragline {
		alias = "dragline"
	}
	if selected != "" && alias != "" && selected != alias {
		return "", fmt.Errorf("conflicting --backend=%s and --%s", selected, alias)
	}
	if selected == "" {
		selected = alias
	}
	return selected, nil
}
