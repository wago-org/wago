// Package profile describes the runtime feature profile selected at build time.
package profile

import "runtime"

func Build() string {
	if runtime.Compiler == "tinygo" {
		return "tiny"
	}
	return "normal"
}
