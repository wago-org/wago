//go:build !linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

// Execution on a non-Linux runner is required; cross-compilation is not a pass.
func TestDesignRepairUnsupportedConvertedPublication(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	g, err := rt.NewExternRefGlobal(NullExternRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	table, err := rt.NewExternRefTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	imp := append(append(wasmtest.Name("p"), wasmtest.Name("g")...), 3, 0x6f, 1)
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 0, 1, 0x6f}, []byte{0x60, 0, 0})),
		wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("convert", 0, 0), wasmtest.ExportEntry("publish", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0xfb, 0x1b, 0x0b}), wasmtest.Code([]byte{0xfb, 1, 0, 0xfb, 0x1b, 0x24, 0, 0x0b}))))
	code, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), raw)
	if !SupportedFeatures().IsEnabled(CoreFeatureGC) {
		// Windows rejects GC at configuration admission. Prove that boundary
		// instead of requiring it to execute an unsupported GC guest. Keep
		// public publication rejection covered with authenticated metadata only;
		// no managed object is constructed on this target.
		var unsupported *UnsupportedFeatureError
		if code != nil || !errors.As(err, &unsupported) || !unsupported.Requested.IsEnabled(CoreFeatureGC) {
			t.Fatalf("GC admission: code=%v err=%v, want GC UnsupportedFeatureError", code, err)
		}
		token, err := rt.refStore.issueExternref(repairPublicGlobalWrapper{})
		if err != nil {
			t.Fatal(err)
		}
		defer rt.refStore.releaseExternref(token)
		if _, ok := rt.refStore.resolveExternref(token); !ok {
			t.Fatal("metadata wrapper must be authenticated")
		}
		value := ValueOf(ValExternRef, token)
		if err := g.SetValue(value); err == nil {
			t.Fatal("unsupported public global accepted converted wrapper")
		}
		if err := table.SetValue(0, value); err == nil {
			t.Fatal("unsupported public table accepted converted wrapper")
		}
		if binary.LittleEndian.Uint64(g.cell) != 0 || binary.LittleEndian.Uint64(table.desc[8:]) != 0 {
			t.Fatal("rejected metadata publication changed destination")
		}
		table.Close()
		g.Close()
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	in, err := instantiateCore(code, InstantiateOptions{store: rt.refStore, Imports: testImports("p.g", g)})
	if err != nil {
		t.Fatal(err)
	}
	values, err := in.InvokeValues(context.Background(), "convert")
	if err != nil || len(values) != 1 {
		t.Fatalf("safe conversion: %v %v", values, err)
	}
	if _, err = in.Invoke("publish"); err == nil {
		t.Fatal("STOP unsafe converted guest publication accepted")
	}
	if binary.LittleEndian.Uint64(g.cell) != 0 {
		t.Fatal("STOP rejected guest store changed destination")
	}
	if err = g.SetValue(values[0]); err == nil {
		t.Fatal("STOP unsafe converted public global accepted")
	}
	if err = table.SetValue(0, values[0]); err == nil {
		t.Fatal("STOP unsafe converted public table accepted")
	}
	if binary.LittleEndian.Uint64(g.cell) != 0 || binary.LittleEndian.Uint64(table.desc[8:]) != 0 {
		t.Fatal("STOP rejected public store changed destination")
	}
	in.Close()
	table.Close()
	g.Close()
	code.Close()
}

func TestDesignRepairUnsupportedBulkAtomicity(t *testing.T) {
	rt := NewRuntime()
	in := &Instance{refStore: rt.refStore}
	ref, err := rt.NewExternRef("ordinary")
	if err != nil {
		t.Fatal(err)
	}
	published := false
	if err = in.publishConversionRangeInvocation(gcConversionInvocation{}, nil, nil, 0, 2, []uint64{ref.token, ^uint64(0)}, func() { published = true }); err == nil || published {
		t.Fatal("partial unsupported bulk publication")
	}
	if err = in.publishConversionRangeInvocation(gcConversionInvocation{}, nil, nil, 0, 2, []uint64{ref.token, 0}, func() { published = true }); err != nil || !published {
		t.Fatalf("ordinary bulk rejected: %v", err)
	}
	original := make([]byte, 16)
	binary.LittleEndian.PutUint32(original, 1)
	staged := append([]byte(nil), original...)
	binary.LittleEndian.PutUint64(staged[8:], ^uint64(0))
	if err = in.initializeConversionReceipts([]conversionInitialTable{{original: original, staged: staged, touched: []bool{true}}}); err == nil || binary.LittleEndian.Uint64(original[8:]) != 0 {
		t.Fatal("unsafe active initialization")
	}
}
