//go:build wagodebug

package gc

import (
	"slices"
	"testing"
)

func TestMatureLedgerPublicMixedFailureRetry(t *testing.T) {
	c, roots, entry := matureLedgerMixedPublic(t)
	cleanup := armFailure(&c.throughput, failThroughputReconciliation, 0)
	err := c.CollectFull(roots)
	cleanup()
	if !isInjectedFailure(err) {
		t.Fatal("public route did not expose reconciliation failure", err)
	}
	d := c.CollectionDiagnostics()
	if d.Result != "failed" || !d.Pressure || !isInjectedFailure(d.RequiredCause) {
		t.Fatal("failure diagnostics", d)
	}
	if c.throughput.pendingBytes != 32 || !slices.Equal(c.throughput.pendingFree, []throughputFreeSpan{{off: 0, size: 32}}) || c.throughput.freeBytes != 0 {
		t.Fatal("lost exact retry debt")
	}
	if *c.entry(roots[0].GetRef()) != entry {
		t.Fatal("LOS changed on failure")
	}
	if err = c.Verify(roots); err != nil {
		t.Fatal(err)
	}
	if err = c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if c.throughput.pendingBytes != 0 || c.throughput.freeBytes != 32 {
		t.Fatal("retry lost debt")
	}
}
