//go:build !wago_manager && !wago_minimal

package wagocli

func runnerProfile() string  { return "standard" }
func runnerBuildTag() string { return "" }

func runnerCommands() []*Cmd {
	return []*Cmd{
		runCommand(),
		initCommand(),
		addCommand(),
		rmCommand(),
		pluginCommand(),
		authCommand(),
		moduleCommand(),
		selfCommand(),
		buildCommand(),
		validateCommand(),
		versionCommand(),
	}
}
