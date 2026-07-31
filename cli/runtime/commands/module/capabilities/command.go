// Package capabilities defines wago module capabilities.
package capabilities

import (
	"fmt"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	runtimemodule "github.com/wago-org/wago/cli/runtime/internal/module"
)

func Command() *command.Cmd {
	return &command.Cmd{
		Name: "capabilities", Aliases: []string{"caps"},
		Summary: "list the capabilities a module requires", Args: "<file>",
		Run: run,
	}
}

func run(c *command.Ctx) {
	rt, mod := runtimemodule.Compile(c.One("<file>"))
	defer rt.Close()
	defer mod.Close()
	caps := mod.RequiredCapabilities()
	if len(caps) == 0 {
		fmt.Println(ui.Dim("module requires no capabilities"))
		return
	}
	fmt.Printf("%s\n", ui.Bold("required capabilities:"))
	for _, capability := range caps {
		fmt.Printf("  %s\n", string(capability))
	}
}
