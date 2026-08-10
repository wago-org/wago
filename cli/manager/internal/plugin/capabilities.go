package plugin

import "strings"

var capabilityDescriptions = map[string]string{
	"host.imports":       "provide host-import functions to guests",
	"host.environment":   "read the host environment (args, env, clock, fs)",
	"module.compile":     "hook module compilation",
	"instance.lifecycle": "hook instance create/destroy",
	"instance.invoke":    "hook guest invocations",
	"runtime.lifecycle":  "hook runtime start/stop",
	"instance.manage":    "create and manage guest instances",
	"runtime.core":       "compile and compose core modules as a trusted execution model",
}

func CapabilityDescription(capability string) string {
	return capabilityDescriptions[capability]
}

func ContainsDependency(dependencies []string, id string) bool {
	for _, dependency := range dependencies {
		if strings.TrimPrefix(dependency, "github.com/") == id {
			return true
		}
	}
	return false
}
