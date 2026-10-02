package handoff

import "github.com/wago-org/wago/cli/internal/command"

// Keep command names and aliases in one immutable-by-convention table. Routing
// consults these tables too, so adding a displayed alias cannot strand it in
// the manager instead of handing it to the runtime.
var (
	pluginListCommandNames    = [...]string{"list", "ls"}
	pluginInspectCommandNames = [...]string{"inspect", "info", "show"}
)

// PluginListCommand describes the runtime-owned plugin list command. The
// manager uses the description for cohesive help; the runtime attaches Run.
func PluginListCommand() *command.Cmd {
	return &command.Cmd{
		Name: pluginListCommandNames[0], Aliases: append([]string(nil), pluginListCommandNames[1:]...),
		Summary:    "list plugins enabled for the selected scope",
		Automation: command.JSONOutput,
		Flags:      pluginInspectionFlags(),
	}
}

// PluginInspectCommand describes the runtime-owned plugin inspect command.
func PluginInspectCommand() *command.Cmd {
	return &command.Cmd{
		Name: pluginInspectCommandNames[0], Aliases: append([]string(nil), pluginInspectCommandNames[1:]...), Summary: "show immutable definition, authorities, and contract bindings", Args: "[plugin-id]",
		Automation: command.JSONOutput,
		Flags:      pluginInspectionFlags(),
	}
}

func pluginInspectionFlags() []command.Flag {
	return []command.Flag{
		{Name: "global", Short: "g", Bool: true, Help: "use the shared user-wide plugins"},
		{Name: "local", Short: "l", Bool: true, Help: "use this project's plugins"},
	}
}
