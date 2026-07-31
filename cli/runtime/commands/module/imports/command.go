// Package imports defines wago module imports.
package imports

import (
	"fmt"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	runtimemodule "github.com/wago-org/wago/cli/runtime/internal/module"
)

func Command() *command.Cmd {
	return &command.Cmd{
		Name: "imports", Summary: "list a module's imports (resolved vs plugins)", Args: "<file>",
		Run: run,
	}
}

func run(c *command.Ctx) {
	rt, mod := runtimemodule.Compile(c.One("<file>"))
	defer rt.Close()
	defer mod.Close()
	imports := mod.Imports()
	if len(imports) == 0 {
		fmt.Println(ui.Dim("module has no imports"))
		return
	}
	fmt.Printf("%s\n", ui.Bold("imports:"))
	for _, spec := range imports {
		mark := ui.Red("unresolved")
		if spec.Provided {
			mark = ui.Cyan("provided")
		}
		line := fmt.Sprintf("  %s  %s  %s", spec.Key(), ui.Dim(spec.Kind.String()), mark)
		if spec.Kind == wago.ImportFunc && (len(spec.Params) > 0 || len(spec.Results) > 0) {
			line += "  " + ui.Dim(signature(spec.Params, spec.Results))
		}
		if spec.HasCapability {
			line += "  " + ui.Dim("["+string(spec.Capability)+"]")
		}
		fmt.Println(line)
	}
}

func signature(params, results []wago.ValType) string {
	ps := make([]string, len(params))
	for i, p := range params {
		ps[i] = p.String()
	}
	sig := "(" + strings.Join(ps, ", ") + ")"
	if len(results) == 0 {
		return sig
	}
	rs := make([]string, len(results))
	for i, result := range results {
		rs[i] = result.String()
	}
	return sig + " -> " + strings.Join(rs, ", ")
}
