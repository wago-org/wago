// Package update defines the coordinated top-level wago update command.
package update

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	Self, Runtime, Plugins  bool
	Channel, Profile, Build string
	Global, Local, Verbose  bool
	Use                     string
}

type Environment interface {
	UpdateEverything(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:    "update",
		Summary: "update Wago, the active runtime, and plugins",
		Flags: []command.Flag{
			{Name: "self", Bool: true, Help: "update only the Wago manager"},
			{Name: "runtime", Bool: true, Help: "update the active release channel"},
			{Name: "plugins", Bool: true, Help: "update enabled plugins"},
			{Name: "all", Bool: true, Help: "update manager, runtime, and plugins"},
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
			all := ctx.Bool("all") || (!ctx.Bool("self") && !ctx.Bool("runtime") && !ctx.Bool("plugins"))
			environment.UpdateEverything(Options{
				Self: all || ctx.Bool("self"), Runtime: all || ctx.Bool("runtime"), Plugins: all || ctx.Bool("plugins"),
				Channel: ctx.Str("channel"), Profile: ctx.Str("profile"), Build: ctx.Str("build"),
				Global: ctx.Bool("global"), Local: ctx.Bool("local"), Verbose: ctx.Bool("verbose"), Use: use,
			})
		},
	}
}
