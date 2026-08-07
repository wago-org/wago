// Package capabilities defines wago module capabilities.
package capabilities

import (
	"fmt"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	runtimemodule "github.com/wago-org/wago/cli/runtime/internal/module"
)

func Command() *command.Cmd {
	return &command.Cmd{
		Name: "capabilities", Aliases: []string{"caps"},
		Summary: "list the capabilities a module requires", Args: "<file>",
		Automation: command.JSONOutput,
		Run:        run,
	}
}

func run(c *command.Ctx) {
	rt, mod := runtimemodule.Compile(c.One("<file>"))
	defer rt.Close()
	defer mod.Close()
	caps := mod.RequiredCapabilities()
	if automation.JSON() {
		values := make([]string, len(caps))
		for index, capability := range caps {
			values[index] = string(capability)
		}
		ui.PrintJSON(map[string]any{"capabilities": values})
		return
	}
	if len(caps) == 0 {
		fmt.Println(ui.Dim("module requires no capabilities"))
		return
	}
	fmt.Printf("%s\n", ui.Bold("required capabilities:"))
	for _, capability := range caps {
		fmt.Printf("  %s\n", string(capability))
	}
}
