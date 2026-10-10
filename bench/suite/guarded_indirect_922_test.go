package wagobench

import (
	"testing"

	"github.com/wago-org/wago"
)

// FastTree's selected site is global function 73, body PC 549, table 0;
// its observed local target is global function 170. Run the same test in
// separate processes with and without WAGO_AMD64_EXPERIMENT_INDIRECT_SITE.
func guardedFastTree(tb testing.TB) corpusModule {
	tb.Helper()
	for _, m := range commandCorpus(tb) {
		if m.ID == "fasttree-phylogeny" {
			return m
		}
	}
	tb.Skip("select fasttree-phylogeny with -wago.corpus=fasttree-phylogeny")
	return corpusModule{}
}

func TestGuardedFastTreeCommand(t *testing.T) {
	m := guardedFastTree(t)
	compiled, err := wago.Compile(nil, m.bytes)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	t.Logf("native code bytes: %d", compiled.CodeSize())
	got, err := runWagoCommand(m, compiled, commandInput(t, m), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCommandOutput(m, got); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkGuardedFastTreeCommand(b *testing.B) {
	m := guardedFastTree(b)
	compiled, err := wago.Compile(nil, m.bytes)
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	got, err := runWagoCommand(m, compiled, commandInput(b, m), true)
	if err != nil {
		b.Fatal(err)
	}
	if err := validateCommandOutput(m, got); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := runWagoCommand(m, compiled, commandInput(b, m), false); err != nil {
			b.Fatal(err)
		}
	}
}
