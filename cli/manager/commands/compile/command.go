// Package compile defines wago compile.
package compile

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
)

type Options struct {
	Input, Output, Target, Invoke string
	Global, Local, Bare           bool
	Verbose                       bool
}

type Environment interface {
	Compile(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:       "compile",
		Summary:    "build a standalone executable from a WebAssembly module",
		Args:       "<file>",
		Automation: command.DryRun,
		Flags: []command.Flag{
			{Name: "output", Short: "o", Arg: "<file>", Help: "output executable path"},
			{Name: "target", Arg: "<os/arch>", Help: "target platform (default: current platform)"},
			{Name: "invoke", Short: "e", Arg: "<name>", Help: "exported function to call"},
			{Name: "global", Short: "g", Bool: true, Help: "include shared user-wide plugins"},
			{Name: "local", Bool: true, Help: "include this project's plugins"},
			{Name: "bare", Bool: true, Help: "build without plugins"},
			{Name: "verbose", Short: "v", Bool: true, Help: "show Go build output"},
		},
		Long: "The executable embeds the module and selected plugin configuration. By default it\n" +
			"calls _start; use --invoke to bake in another exported function. Use --target\n" +
			"linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64,\n" +
			"or windows/arm64 to cross-compile with the matching Wago backend.",
		Run: func(context *command.Ctx) {
			if len(context.Args) != 1 {
				ui.Usage("compile: need exactly one <file>")
			}
			environment.Compile(Options{
				Input: context.Args[0], Output: context.Str("output"), Target: context.Str("target"), Invoke: context.Str("invoke"),
				Global: context.Bool("global"), Local: context.Bool("local"), Bare: context.Bool("bare"),
				Verbose: context.Bool("verbose"),
			})
		},
	}
}
