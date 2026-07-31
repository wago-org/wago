package handoff

import "github.com/wago-org/wago/cli/internal/command"

// PluginListCommand describes the runtime-owned plugin list command. The
// manager uses the description for cohesive help; the runtime attaches Run.
func PluginListCommand() *command.Cmd {
	return &command.Cmd{
		Name: "list", Aliases: []string{"ls"},
		Summary: "list plugins enabled for the selected scope",
		Flags:   pluginInspectionFlags(),
	}
}

// PluginInspectCommand describes the runtime-owned plugin inspect command.
func PluginInspectCommand() *command.Cmd {
	return &command.Cmd{
		Name: "inspect", Summary: "show an enabled plugin's imports and capabilities", Args: "<name>",
		Flags: pluginInspectionFlags(),
	}
}

func pluginInspectionFlags() []command.Flag {
	return []command.Flag{
		{Name: "global", Short: "g", Bool: true, Help: "use the shared user-wide plugins"},
		{Name: "local", Short: "l", Bool: true, Help: "use this project's plugins"},
		{Name: "json", Bool: true, Help: "emit machine-readable JSON"},
	}
}
