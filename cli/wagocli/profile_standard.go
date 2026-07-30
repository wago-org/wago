//go:build !wago_manager && !wago_lite && !wago_minimal

package wagocli

func runnerProfile() string  { return "standard" }
func runnerBuildTag() string { return "" }

func runnerCommands() []*Cmd {
	return []*Cmd{
		runCommand(),
		addCommand(),
		rmCommand(),
		pluginCommand(),
		authCommand(),
		moduleCommand(),
		optsCommand(),
		buildCommand(),
		validateCommand(),
		versionCommand(),
	}
}
