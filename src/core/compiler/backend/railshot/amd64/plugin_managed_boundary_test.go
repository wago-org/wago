//go:build amd64

package amd64

import (
	"testing"

	plugincodegen "github.com/wago-org/wago/codegen/amd64"
)

func TestManagedPluginContextDoesNotExposeFullAMD64Context(t *testing.T) {
	var managed plugincodegen.ManagedContext = &pluginAMD64Context{}
	if _, ok := managed.(plugincodegen.Context); ok {
		t.Fatal("managed plugin context exposes the full AMD64 encoder context")
	}
}
