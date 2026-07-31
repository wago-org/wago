// Package initcmd defines wago init.
package initcmd

import (
	"fmt"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/cli/internal/ui"
)

func Command() *command.Cmd {
	return &command.Cmd{
		Name: "init", Summary: "initialize a local Wago project",
		Run: func(*command.Ctx) {
			created, err := project.Initialize(".")
			if err != nil {
				ui.Fatal("init: %v", err)
			}
			project.EnsureGitignore(".wago/")
			if created {
				fmt.Println(ui.Cyan("Initialized wago.json"))
				return
			}
			fmt.Println(ui.Dim("wago.json already initialized"))
		},
	}
}
