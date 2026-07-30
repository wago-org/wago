//go:build !wago_manager && wago_lite && !wago_minimal

package wagocli

func runnerProfile() string  { return "lite" }
func runnerBuildTag() string { return "wago_lite" }

func runnerCommands() []*Cmd {
	return []*Cmd{
		runCommand(),
		buildCommand(),
		addCommand(),
		rmCommand(),
		pluginCommand(),
	}
}
