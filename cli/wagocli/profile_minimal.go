//go:build !wago_manager && wago_minimal

package wagocli

func runnerProfile() string  { return "minimal" }
func runnerBuildTag() string { return "wago_minimal" }

func runnerCommands() []*Cmd { return []*Cmd{runCommand()} }
