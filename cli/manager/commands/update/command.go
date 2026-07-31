// Package update defines the coordinated top-level wago update command.
package update

import (
	"fmt"
	"strings"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Components struct {
	Manager bool
	Runtime bool
	Plugins bool
}

type Options struct {
	Components
	Channel, Profile, Build string
	Global, Local, Verbose  bool
	Force                   bool
	Use                     string
}

type Environment interface {
	UpdateEverything(Options)
}

func Command(environment Environment) *command.Cmd {
	return commandWithSelector(environment, SelectComponents)
}

type componentSelector func() (Components, bool, error)

func commandWithSelector(environment Environment, selectComponents componentSelector) *command.Cmd {
	return &command.Cmd{
		Name:    "update",
		Summary: "update Wago, the active runtime, and plugins",
		Args:    "[target...]",
		Flags: []command.Flag{
			{Name: "manager", Bool: true, Help: "update the Wago manager"},
			{Name: "runtime", Bool: true, Help: "update the active release channel"},
			{Name: "plugins", Bool: true, Help: "update enabled plugins"},
			{Name: "all", Bool: true, Help: "update manager, runtime, and plugins"},
			{Name: "force", Short: "f", Bool: true, Help: "update even when installed commits match"},
			{Name: "channel", Arg: "<name>", Help: "runtime channel: canary or nightly"},
			{Name: "profile", Arg: "<name>", Help: "runtime profile"},
			{Name: "build", Arg: "<name>", Help: "runtime build"},
			plugin.GlobalFlag(),
			plugin.LocalFlag(),
			{Name: "verbose", Short: "v", Bool: true, Help: "stream plugin build output"},
			{Name: "use", Bool: true, Help: "make the updated runtime active"},
			{Name: "no-use", Bool: true, Help: "do not change the active runtime"},
		},
		Run: func(ctx *command.Ctx) {
			if ctx.Bool("global") && ctx.Bool("local") {
				ui.Fatal("update: choose --global or --local")
			}
			use := "yes"
			if ctx.Bool("no-use") {
				use = "no"
			}
			if ctx.Bool("use") && ctx.Bool("no-use") {
				ui.Fatal("update: choose --use or --no-use")
			}
			components, explicit := componentsFromContext(ctx)
			if !explicit {
				var submitted bool
				var err error
				components, submitted, err = selectComponents()
				if err != nil {
					ui.Fatal("update: %v", err)
				}
				if !submitted {
					fmt.Println("Cancelled.")
					return
				}
				if !components.Manager && !components.Runtime && !components.Plugins {
					fmt.Println("No updates selected.")
					return
				}
			}
			environment.UpdateEverything(Options{
				Components: components,
				Channel:    ctx.Str("channel"), Profile: ctx.Str("profile"), Build: ctx.Str("build"),
				Global: ctx.Bool("global"), Local: ctx.Bool("local"), Verbose: ctx.Bool("verbose"), Force: ctx.Bool("force"), Use: use,
			})
		},
	}
}

func componentsFromContext(ctx *command.Ctx) (Components, bool) {
	components := Components{
		Manager: ctx.Bool("manager"),
		Runtime: ctx.Bool("runtime"),
		Plugins: ctx.Bool("plugins"),
	}
	explicit := components.Manager || components.Runtime || components.Plugins
	if ctx.Bool("all") {
		components = allComponents()
		explicit = true
	}
	for _, raw := range ctx.Args {
		switch strings.ToLower(raw) {
		case "manager":
			components.Manager = true
		case "runtime":
			components.Runtime = true
		case "plugin", "plugins":
			components.Plugins = true
		case "all":
			components = allComponents()
		default:
			ui.Fatal("update: unknown target %q; choose manager, runtime, plugins, or all", raw)
		}
		explicit = true
	}
	return components, explicit
}

func allComponents() Components {
	return Components{Manager: true, Runtime: true, Plugins: true}
}
