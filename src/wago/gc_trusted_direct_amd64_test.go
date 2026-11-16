//go:build linux && amd64 && (wago_gc_direct_trusted || wago_gc_direct_trusted_store || wago_gc_direct_trusted_arrays)

package wago

import (
	"bytes"
	"context"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
)

func TestTrustedDirectReceiptAndPrivateSnapshot(t *testing.T) {
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), directPublicRootsModule())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	snapshot := c.executionView()
	if snapshot == c || (snapshot.gcTrustedDirectVersion < 1 || snapshot.gcTrustedDirectVersion > 3) || snapshot.gcDirectReadABIReceipt() != gc.DirectABIVersion || snapshot.hasGCHelperOnlyReceipt() {
		t.Fatal("missing private trusted snapshot")
	}
	before, e := compiledMetadataBytes(snapshot, false)
	if e != nil {
		t.Fatal(e)
	}
	saved := snapshot.gcTrustedDirectVersion
	snapshot.gcTrustedDirectVersion = 0
	after, e := compiledMetadataBytes(snapshot, false)
	snapshot.gcTrustedDirectVersion = saved
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("trusted receipt serialized", e)
	}
	rt := NewRuntime()
	defer rt.Close()
	cfg := InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true}}
	for _, bad := range []string{"version", "abi", "helper"} {
		v, abi, helper := snapshot.gcTrustedDirectVersion, snapshot.gcDirectReadABIVersion, snapshot.gcHelperOnlyReceipt
		switch bad {
		case "version":
			snapshot.gcTrustedDirectVersion = 255
		case "abi":
			snapshot.gcDirectReadABIVersion = 0
		case "helper":
			snapshot.gcHelperOnlyReceipt = true
		}
		in, e := Instantiate(c, cfg)
		snapshot.gcTrustedDirectVersion, snapshot.gcDirectReadABIVersion, snapshot.gcHelperOnlyReceipt = v, abi, helper
		if e == nil {
			in.Close()
			t.Fatal("admitted bad trusted receipt", bad)
		}
	}
	// Public aliases/copies cannot replace the private frozen execution metadata.
	copied := *c
	copied.GCTypeDescs[0].Fields[0].Offset = 4096
	copied.Exports["new"] = 99999
	in, e := Instantiate(&copied, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	values, e := in.InvokeValues(context.Background(), "new", ValueI32(91))
	if e != nil || len(values) != 1 {
		t.Fatal(values, e)
	}
	defer in.ReleaseGCRef(values[0].GCRef())
	if e = in.SetGlobalValue("root", values[0]); e != nil {
		t.Fatal(e)
	}
	got, e := in.Invoke("read")
	if e != nil || len(got) != 1 || got[0] != 91 {
		t.Fatal("public mutation reached execution snapshot", got, e)
	}
}

func TestTrustedDirectNullHasPrecisePC(t *testing.T) {
	data := directPublicRootsModule()
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	rt := NewRuntime()
	defer rt.Close()
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true}})
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	_, e = in.Invoke("read")
	var trap *TrapError
	if !errors.As(e, &trap) {
		t.Fatal("null not trapped", e)
	}
	m, e := wasm.DecodeModule(data)
	if e != nil {
		t.Fatal(e)
	}
	pc := uint32(bytes.Index(m.Code[1].BodyBytes, []byte{0xfb, 2, 0, 0})) + m.Code[1].LocalDeclBytes
	if len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != pc {
		t.Fatalf("wrong null PC %+v want%d", trap.Frames, pc)
	}
}

func TestTrustedDirectCloneIngressRejectsWrongEdges(t *testing.T) {
	cfg := GCConfig{ExperimentalDirectOffsets: true, CollectEveryAlloc: true, ForceMajorEveryMinor: true, NurseryBytes: 256, ThroughputHeapBytes: 64 << 10}
	source := newForeignCloneBoundaryInstance(t, foreignCloneEdgeModule(), cfg, nil)
	target := newForeignCloneBoundaryInstance(t, foreignCloneEdgeModule(), cfg, nil)
	token := foreignCloneSourceToken(t, source)
	cloned, e := target.CloneGCRefFrom(source, token)
	if e != nil {
		t.Fatal(e)
	}
	target.ReleaseGCRef(cloned)
	// Construct malformed source input only to exercise checked clone ingress;
	// never execute trusted field loads against this deliberately corrupted graph.
	wrong, e := source.gc.NewStructDefaultWithRoots(source.requireGCDomainType(2), gc.EmptyRoots{})
	if e != nil {
		t.Fatal(e)
	}
	root := cloneEdgeRawRoot(t, source, token)
	for _, bad := range []gc.Ref{wrong, gc.I31New(1)} {
		if e = source.gc.StructSet(root, 0, gc.RefValue(bad)); e != nil {
			t.Fatal(e)
		}
		if cloned, e = target.CloneGCRefFrom(source, token); e == nil {
			target.ReleaseGCRef(cloned)
			t.Fatal("wrong edge crossed trusted clone ingress")
		}
	}
}
