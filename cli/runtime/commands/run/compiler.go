package run

import (
	"fmt"

	"github.com/wago-org/wago/cli/internal/command"
)

// CompilerFlags exposes canonical engine selection and the two convenience
// aliases. Selection is strict; --dragline does not imply fallback.
func CompilerFlags() []command.Flag {
	return []command.Flag{
		{Name: "compiler", Arg: "<engine>", Help: "compiler engine: railshot | dragline (default railshot)"},
		{Name: "railshot", Bool: true, Help: "use the Railshot compiler"},
		{Name: "dragline", Bool: true, Help: "use the Dragline compiler (strict; no fallback)"},
	}
}

// CompilerOverride resolves canonical and convenience engine flags.
func CompilerOverride(ctx *command.Ctx) (string, error) {
	selected := ctx.Str("compiler")
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
		return "", fmt.Errorf("conflicting --compiler=%s and --%s", selected, alias)
	}
	if selected == "" {
		selected = alias
	}
	return selected, nil
}
