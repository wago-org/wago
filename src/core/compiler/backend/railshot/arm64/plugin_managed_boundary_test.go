//go:build arm64

package arm64

import (
	"testing"

	plugincodegen "github.com/wago-org/wago/codegen/arm64"
)

func TestManagedPluginContextDoesNotExposeFullARM64Context(t *testing.T) {
	var managed plugincodegen.ManagedContext = (*managedPluginARM64Context)(&pluginARM64Context{})
	if _, ok := managed.(plugincodegen.Context); ok {
		t.Fatal("managed plugin context exposes the full ARM64 encoder context")
	}
}
