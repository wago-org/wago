package plugin

import (
	"testing"

	"github.com/wago-org/wago/internal/wagopaths"
)

func TestGlobalPluginDirectoryIsSharedV1Intent(t *testing.T) {
	dirs := wagopaths.Dirs{Data: t.TempDir()}
	if got := sharedGlobalPluginDir(dirs); got != dirs.Data {
		t.Fatalf("sharedGlobalPluginDir = %q, want %q", got, dirs.Data)
	}
}
