package handoff

import "github.com/wago-org/wago/cli/internal/command"

// PluginListCommand describes the runtime-owned plugin list command. The
// manager uses the description for cohesive help; the runtime attaches Run.
func PluginListCommand() *command.Cmd {
	return &command.Cmd{
		Name: "list", Aliases: []string{"ls"},
		Summary:    "list plugins enabled for the selected scope",
		Automation: command.JSONOutput,
		Flags:      pluginInspectionFlags(),
	}
}

// PluginInspectCommand describes the runtime-owned plugin inspect command.
func PluginInspectCommand() *command.Cmd {
	return &command.Cmd{
		Name: "inspect", Aliases: []string{"info", "show"}, Summary: "show immutable definition, authorities, and contract bindings", Args: "[plugin-id]",
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
