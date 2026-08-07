// Package handoff defines the contract between the Wago manager and runtime.
package handoff

import "os"

const (
	managerVersionEnv    = "WAGO_MANAGER_VERSION"
	managerExecutableEnv = "WAGO_MANAGER_EXECUTABLE"
	runtimeChannelEnv    = "WAGO_RUNTIME_CHANNEL"
	runtimeProfileEnv    = "WAGO_RUNTIME_PROFILE"
	runtimeBuildEnv      = "WAGO_RUNTIME_BUILD"
)

// Metadata describes the manager and selected runtime involved in a launch.
type Metadata struct {
	ManagerVersion    string
	ManagerExecutable string
	RuntimeChannel    string
	RuntimeProfile    string
	RuntimeBuild      string
}

// FromEnvironment reads metadata supplied by the manager.
func FromEnvironment() Metadata {
	return Metadata{
		ManagerVersion:    os.Getenv(managerVersionEnv),
		ManagerExecutable: os.Getenv(managerExecutableEnv),
		RuntimeChannel:    os.Getenv(runtimeChannelEnv),
		RuntimeProfile:    os.Getenv(runtimeProfileEnv),
		RuntimeBuild:      os.Getenv(runtimeBuildEnv),
	}
}

// Environment returns base with this handoff's non-empty fields appended.
func (m Metadata) Environment(base []string) []string {
	env := append([]string(nil), base...)
	env = appendField(env, managerVersionEnv, m.ManagerVersion)
	env = appendField(env, managerExecutableEnv, m.ManagerExecutable)
	env = appendField(env, runtimeChannelEnv, m.RuntimeChannel)
	env = appendField(env, runtimeProfileEnv, m.RuntimeProfile)
	env = appendField(env, runtimeBuildEnv, m.RuntimeBuild)
	return env
}

func appendField(env []string, name, value string) []string {
	if value == "" {
		return env
	}
	return append(env, name+"="+value)
}
