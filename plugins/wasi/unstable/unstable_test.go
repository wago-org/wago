package unstable_test

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/plugins/wasi/unstable"
)

// TestImportsUseUnstableModule checks the bundle binds under the older
// "wasi_unstable" module name (not wasi_snapshot_preview1) while still covering
// the core functions.
func TestImportsUseUnstableModule(t *testing.T) {
	im := unstable.Imports(unstable.Config{})
	if len(im) == 0 {
		t.Fatal("no imports")
	}
	for k := range im {
		if !strings.HasPrefix(k, unstable.Module+".") {
			t.Errorf("import %q not under module %q", k, unstable.Module)
		}
	}
	for _, name := range []string{"fd_write", "proc_exit", "clock_time_get", "random_get"} {
		if _, ok := im[unstable.Module+"."+name]; !ok {
			t.Errorf("missing %s.%s", unstable.Module, name)
		}
	}
}
