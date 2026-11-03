//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo && !wago_target_tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"
)

// Preserve failed objects without cleanup or further heap access after a failed
// ownership assertion. This probe distinguishes opaque conversion identity from
// an ordinary externref directory token before exercising publication.
var conversionClassificationStopped []*Instance

func TestConvertedMake42ClassificationAndReceipt(t *testing.T) {
	const initialized = "060b016f014107fb0000fb1b0b"
	if strings.Count(externGlobalHostWasm, initialized) != 1 {
		t.Fatal("fixture changed")
	}
	encoded := strings.Replace(externGlobalHostWasm, initialized, "0606016f01d06f0b", 1)
	data, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	conversionClassificationStopped = append(conversionClassificationStopped, in)
	g, err := in.ExportedGlobalObject("g")
	if err != nil {
		t.Fatal(err)
	}
	made, err := in.InvokeValues(context.Background(), "make", ValueI32(42))
	if err != nil || len(made) != 1 {
		t.Fatalf("make(42): %v, %v", made, err)
	}
	s := in.existingGCExternConversionState()
	if s == nil || made[0].typ != ValExternRef {
		t.Fatal("STOP missing conversion state or wrong public type")
	}
	s.mu.Lock()
	var entry gcExternConversionEntry
	for _, candidate := range s.entries {
		if candidate.publicExternWord == made[0].bits {
			entry = candidate
			break
		}
	}
	s.mu.Unlock()
	if entry.kind != gcExternConversionData || !entry.ref.IsObj() || !entry.hasRoot || entry.publicExternWord == entry.externWord {
		t.Fatal("STOP make(42) is not a rooted opaque object conversion")
	}
	if _, ordinary := in.refStore.resolveExternref(made[0].bits); ordinary {
		t.Fatal("STOP converted result misclassified as ordinary externref")
	}
	internal, handled, err := in.internalExternForGlobal(made[0].bits)
	if err != nil || !handled || internal != entry.externWord {
		t.Fatalf("STOP legacy ingress classification: handled=%t err=%v", handled, err)
	}
	t.Log("make42 classification: externref / opaque public conversion / managed object / absent from ordinary directory / recognized by legacy global ingress")
	if err := g.SetValue(made[0]); err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	var receipt *gcConversionReceipt
	h2WithAuthority(t, in.refStore, func() {
		container := in.refStore.gcDomains.conversionContainers[&g.cell[0]]
		if container == nil {
			t.Fatal("STOP publication has no physical container")
		}
		receipt = h2AssertReceipt(t, container, 0)
		if receipt.identity.word != internal || receipt.identity.ref != entry.ref {
			t.Fatal("STOP publication receipt has wrong identity")
		}
	})
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	if got := invokeI32(t, in, "readg"); got != 42 {
		t.Fatalf("post-collection global = %d, want 42", got)
	}
	if err := g.SetValue(ValueExternRef(NullExternRef())); err != nil {
		t.Fatal(err)
	}
	if !receipt.released {
		t.Fatal("STOP clear retained publication receipt")
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if in.refStore.gcDomainForCollector(receipt.identity.domain.collector) != nil {
		t.Fatal("STOP final close retained conversion domain")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	conversionClassificationStopped[len(conversionClassificationStopped)-1] = nil
}

func TestConvertedInitializer55PersistentRoot(t *testing.T) {
	// Same original positive fixture; change only the signed-LEB i32 constant.
	encoded := strings.Replace(externGlobalHostWasm, "4107fb0000", "4137fb0000", 1)
	data, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	conversionClassificationStopped = append(conversionClassificationStopped, in)
	g, err := in.ExportedGlobalObject("g")
	if err != nil {
		t.Fatal(err)
	}
	h2WithAuthority(t, in.refStore, func() {
		container := in.refStore.gcDomains.conversionContainers[&g.cell[0]]
		if container == nil {
			t.Fatal("STOP initialized global has no physical receipt")
		}
		h2AssertReceipt(t, container, 0)
	})
	if got := invokeI32(t, in, "readg"); got != 55 {
		t.Fatalf("initializer = %d, want 55", got)
	}
	// Initializer-only staged execution need not create a public frame state.
	// Drive the production receipt graph collection boundary directly.
	state, ok := managedFunctionGraphs.Load(in.refStore)
	if !ok {
		t.Fatal("STOP initializer has no owner graph")
	}
	graph := state.(*managedFunctionGraph)
	graph.pendingCollection.Store(true)
	graph.drainAfterGate(0)
	if graph.pendingCollection.Load() {
		t.Fatal("initializer graph collection was not serviced")
	}
	if got := invokeI32(t, in, "readg"); got != 55 {
		t.Fatalf("post-collection initializer = %d, want 55", got)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	conversionClassificationStopped[len(conversionClassificationStopped)-1] = nil
}
