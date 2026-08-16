// Package add defines wago add and wago plugin add.
package add

import (
	"strings"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/cli/internal/ui"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	Modules         []string
	Global, Local   bool
	Force, Verbose  bool
	Authorities     []string
	GrantAll        bool
	DenyAll         bool
	AcceptContracts bool
	Scopes          map[string]map[string]project.AuthorityScope
}

type Environment interface {
	Add(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "add", Summary: "add and enable plugins, then rebuild Wago",
		Long:       "GitHub plugins may use owner/repository[/subpackage] shorthand. Package roots offer everything or selected subpackages interactively; --no-input installs everything.",
		Automation: command.DryRun,
		Args:       "<plugin-id>[@range]...",
		Flags: []command.Flag{
			plugin.GlobalFlag(), plugin.LocalFlag(),
			{Name: "force", Short: "f", Bool: true, Help: "ignore the build cache / fetch the latest version"},
			{Name: "verbose", Short: "v", Bool: true, Help: "stream the underlying go output"},
			{Name: "allow", Arg: "<authority,...>", Help: "grant these optional authorities without prompting"},
			{Name: "allow-all", Bool: true, Help: "grant every requested optional authority without prompting"},
			{Name: "deny-all", Bool: true, Help: "deny every optional authority without prompting"},
			{Name: "accept-contracts", Bool: true, Help: "accept the proposed exact contract bindings without prompting"},
			{Name: "scopes", Arg: "<json>", Help: "set narrower scopes by Plugin ID and exact Authority"},
		},
		Run: func(c *command.Ctx) {
			explicitChoices := 0
			for _, selected := range []bool{c.Str("allow") != "", c.Bool("allow-all"), c.Bool("deny-all")} {
				if selected {
					explicitChoices++
				}
			}
			if explicitChoices > 1 {
				ui.Usage("add: choose only one of --allow, --allow-all, or --deny-all")
			}
			scopes, err := plugin.ParseAuthorityScopeOverrides(c.Str("scopes"))
			if err != nil {
				ui.Usage("add: %v", err)
			}
			modules := make([]string, len(c.Args))
			for i, module := range c.Args {
				modules[i] = expandGitHubPluginSpec(module)
			}
			options := Options{
				Modules: modules, Global: c.Bool("global"), Local: c.Bool("local"),
				Force: c.Bool("force"), Verbose: c.Bool("verbose"),
				Authorities:     plugin.SplitCommaList(c.Str("allow")),
				GrantAll:        c.Bool("allow-all"),
				DenyAll:         c.Bool("deny-all"),
				AcceptContracts: c.Bool("accept-contracts"),
				Scopes:          scopes,
			}
			if len(options.Modules) == 0 {
				ui.Usage("add: need at least one <plugin-id>[@range]")
			}
			environment.Add(options)
		},
	}
}

func expandGitHubPluginSpec(spec string) string {
	id := spec
	if index := strings.LastIndexByte(spec, '@'); index > 0 {
		id = spec[:index]
	}
	if project.ValidatePluginID(id) == nil || !strings.Contains(id, "/") {
		return spec
	}
	candidate := "github.com/" + id
	if project.ValidatePluginID(candidate) != nil {
		return spec
	}
	return "github.com/" + spec
}
