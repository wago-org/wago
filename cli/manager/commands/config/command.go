// Package config defines wago config.
package config

import (
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/manager/commands/config/completions"
	configdiff "github.com/wago-org/wago/cli/manager/commands/config/diff"
	configget "github.com/wago-org/wago/cli/manager/commands/config/get"
	configlist "github.com/wago-org/wago/cli/manager/commands/config/list"
	"github.com/wago-org/wago/cli/manager/commands/config/options"
	configreset "github.com/wago-org/wago/cli/manager/commands/config/reset"
	configset "github.com/wago-org/wago/cli/manager/commands/config/set"
)

func Command(environment interface {
	completions.Environment
	options.Environment
}) *command.Cmd {
	return &command.Cmd{
		Name: "config", Summary: "configure Wago defaults and experimental features",
		Automation: command.JSONOutput | command.DryRun,
		Flags: append([]command.Flag{
			{Name: "list", Bool: true, Help: "print the current configuration instead of opening the TUI"},
			{Name: "experimental", Short: "x", Bool: true, Help: "open or include the experimental feature preview"},
			{Name: "enable", Short: "e", Arg: "<setting>", Help: "enable one feature or optimization"},
			{Name: "disable", Short: "d", Arg: "<setting>", Help: "disable one feature or optimization"},
			{Name: "set", Short: "s", Arg: "<key=value>", Help: "set one configuration value"},
			{Name: "reset", Short: "r", Arg: "<setting>", Help: "reset one setting in the selected scope"},
			{Name: "reset-all", Bool: true, Help: "restore every setting in the selected scope"},
		}, options.ScopeFlags()...),
		Children: []*command.Cmd{
			configlist.Command(environment), configdiff.Command(environment), configget.Command(environment), configset.Command(environment),
			configreset.Command(environment), completions.Command(environment),
		},
		Run: func(ctx *command.Ctx) {
			request := options.Request{Action: options.Interactive, Experimental: ctx.Bool("experimental")}
			if err := options.ApplyScope(ctx, &request); err != nil {
				ui.Usage("config: %v", err)
			}
			actions := 0
			if ctx.Bool("list") {
				request.Action = options.List
				actions++
			}
			for _, item := range []struct {
				name, value, state string
			}{
				{"enable", ctx.Str("enable"), "on"}, {"disable", ctx.Str("disable"), "off"},
			} {
				if item.value != "" {
					request.Action, request.Key, request.Value = options.Set, item.value, item.state
					actions++
				}
			}
			if value := ctx.Str("set"); value != "" {
				key, setting, ok := strings.Cut(value, "=")
				if !ok || strings.TrimSpace(key) == "" {
					ui.Usage("config: --set needs <key=value>")
				}
				request.Action, request.Key, request.Value = options.Set, key, setting
				actions++
			}
			if key := ctx.Str("reset"); key != "" {
				request.Action, request.Key = options.Reset, key
				actions++
			}
			if ctx.Bool("reset-all") {
				request.Action, request.All = options.Reset, true
				actions++
			}
			if actions == 0 && (automation.JSON() || automation.NoInput()) {
				request.Action = options.List
				actions++
			}
			if actions > 1 {
				ui.Usage("config: choose one action")
			}
			environment.Configure(request)
		},
	}
}
