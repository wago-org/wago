package wago

import (
	"strings"
	"testing"
)

func TestFailedInstantiationReleasesCodeAndImportState(t *testing.T) {
	compiled := MustCompile(failingLocalStartModule())
	defer compiled.Close()
	imports := Imports{"env.f": HostFunc(func(HostModule, []uint64, []uint64) {})}

	for attempt := 0; attempt < 8; attempt++ {
		instance, err := Instantiate(compiled, imports)
		if instance != nil || err == nil || !strings.Contains(err.Error(), "start function trapped") {
			t.Fatalf("attempt %d: Instantiate = %p, %v; want nil start trap", attempt, instance, err)
		}
		compiled.codeCache.mu.Lock()
		refs := compiled.codeCache.refs
		compiled.codeCache.mu.Unlock()
		if refs != 0 {
			t.Fatalf("attempt %d: executable mapping refs = %d, want 0 after rollback", attempt, refs)
		}
	}
}
