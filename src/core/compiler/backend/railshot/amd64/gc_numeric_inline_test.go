//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCNumericInlineLeafBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  []byte
		local []byte
		want  bool
	}{
		{"add", []byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}, nil, true},
		{"division", []byte{0x41, 42, 0x20, 0, 0x6d, 0x0b}, nil, true},
		{"select", []byte{0x41, 1, 0x41, 2, 0x20, 0, 0x1b, 0x0b}, nil, true},
		{"integer-local", []byte{0x20, 0, 0x21, 1, 0x20, 1, 0x0b}, []byte{1, 1, 0x7f}, true},
		{"float-local", []byte{0x20, 0, 0x0b}, []byte{1, 1, 0x7d}, false},
		{"reference-local", []byte{0x20, 0, 0x0b}, []byte{1, 1, 0x6d}, false},
		{"call", []byte{0x20, 0, 0x10, 0, 0x0b}, nil, false},
		{"memory", []byte{0x20, 0, 0x28, 2, 0, 0x0b}, nil, false},
		{"global", []byte{0x23, 0, 0x0b}, nil, false},
		{"allocation", []byte{0xfb, 1, 0, 0x1a, 0x20, 0, 0x0b}, nil, false},
		{"if", []byte{0x20, 0, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 2, 0x0b, 0x0b}, nil, false},
		{"loop", []byte{0x03, 0x40, 0x0b, 0x20, 0, 0x0b}, nil, false},
		{"unreachable", []byte{0x00, 0x0b}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			locals := tc.local
			if locals == nil {
				locals = []byte{0}
			}
			body := append(append([]byte(nil), locals...), tc.body...)
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
				wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))),
				wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
			)
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			if got := gcNumericInlineLeaf(m, 0); got != tc.want {
				t.Fatalf("numeric leaf = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestGCExactRootTinyReferenceLeafInline(t *testing.T) {
	requireCompilerDiagnostics(t)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x50, 0, 0x5f, 0},             // type 0: open empty struct
			[]byte{0x50, 1, 0, 0x5f, 0},          // type 1: final subtype of type 0
			[]byte{0x60, 1, 0x64, 0, 1, 0x64, 1}, // type 2: (ref 0) -> (ref 1)
			[]byte{0x60, 1, 0x64, 1, 1, 0x64, 1}, // type 3: (ref 1) -> (ref 1)
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfb, 22, 1, 0x0b}), // tiny reference leaf
			wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}),     // statically proved caller
		)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	meta, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	plans := shared.NewGCModuleFrameRootPlan(2)
	if !plans.MarkFunction(1) || !plans.ReserveFunctions(1) {
		t.Fatal("prepare exact root plan")
	}
	plan, ok := plans.BeginFunction(1)
	if !ok {
		t.Fatal("begin exact root plan")
	}
	plan.Candidate = true
	if !plan.SetLiveMasks([]uint64{0}, 0, 1) {
		t.Fatal("set source call mask")
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{
		Workers:         1,
		GCFrameRoots:    plans,
		GCStructHelpers: true,
		GCArrayHelpers:  true,
		Stats:           &stats,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs: meta.Descs, GCTypeLayouts: meta.Layouts,
		}},
	})
	if cm != nil && cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[1].Calls[callKindInline]; got != 1 {
		t.Fatalf("tiny reference inline calls=%d want1 (calls=%v)", got, stats.Funcs[1].Calls)
	}
	if got := stats.Funcs[1].Peephole["inline-reference-param-forward"]; got != 1 {
		t.Fatalf("tiny reference parameter forwards=%d want1", got)
	}
	if got := stats.Funcs[1].Peephole["gc-static-cast-call-elide"]; got != 1 {
		t.Fatalf("tiny reference cast elisions=%d want1 (calls=%v)", got, stats.Funcs[1].Calls)
	}
	if calls := stats.Funcs[1].Calls; calls["gcnative"]+calls["gcnative-leaf"] != 0 {
		t.Fatalf("proved tiny reference leaf emitted a runtime check: %v", calls)
	}
	if !plan.Exact {
		t.Fatal("tiny reference inline lost exact root plan")
	}
	if got := plan.CallsiteCount(); got != 0 {
		t.Fatalf("native callsites=%d want0", got)
	}
}

func TestGCExactRootGeneralTinyLeafInline(t *testing.T) {
	requireCompilerDiagnostics(t)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0}, // type 0: empty struct
			[]byte{0x60, 3, 0x7f, 0x63, 0, 0x63, 0, 1, 0x63, 0}, // type 1: (i32, ref null 0, ref null 0) -> (ref null 0)
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 2, 0x0b}),                            // tiny general leaf
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x10, 0, 0x0b}), // exact-root caller
		)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	meta, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	plans := shared.NewGCModuleFrameRootPlan(2)
	if !plans.MarkFunction(1) || !plans.ReserveFunctions(1) {
		t.Fatal("prepare exact root plan")
	}
	plan, ok := plans.BeginFunction(1)
	if !ok {
		t.Fatal("begin exact root plan")
	}
	plan.Candidate = true
	if !plan.SetLiveMasks([]uint64{6}, 0, 1) {
		t.Fatal("set source call mask")
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{
		Workers:         1,
		GCFrameRoots:    plans,
		GCStructHelpers: true,
		GCArrayHelpers:  true,
		Stats:           &stats,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs: meta.Descs, GCTypeLayouts: meta.Layouts,
		}},
	})
	if cm != nil && cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[1].Calls[callKindInline]; got != 1 {
		t.Fatalf("general tiny leaf inline calls=%d want1 (calls=%v)", got, stats.Funcs[1].Calls)
	}
	if !plan.Exact {
		t.Fatal("general tiny leaf lost exact root plan")
	}
	if got := plan.CallsiteCount(); got != 0 {
		t.Fatalf("native callsites=%d want0", got)
	}
}

func TestGCExactRootTinyReferenceLeafClasses(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name       string
		body       []byte
		wantInline int
		wantCalls  int
	}{
		{name: "reference-constant", body: []byte{0xd0, 0, 0x0b}, wantInline: 1},
		{name: "allocation", body: []byte{0xfb, 1, 0, 0x0b}, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(
					[]byte{0x5f, 0},             // type 0: empty struct
					[]byte{0x60, 0, 1, 0x63, 0}, // type 1: () -> (ref null 0)
				)),
				wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})),
				wasmtest.Section(10, wasmtest.Vec(
					wasmtest.Code(tc.body),
					wasmtest.Code([]byte{0x10, 0, 0x0b}),
				)),
			)
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err = wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			meta, err := frontend.BuildGCTypeMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			plans := shared.NewGCModuleFrameRootPlan(2)
			if !plans.MarkFunction(1) || !plans.ReserveFunctions(1) {
				t.Fatal("prepare exact root plan")
			}
			plan, ok := plans.BeginFunction(1)
			if !ok {
				t.Fatal("begin exact root plan")
			}
			plan.Candidate = true
			if !plan.SetLiveMasks([]uint64{0}, 0, 1) {
				t.Fatal("set source call mask")
			}
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{
				Workers:         1,
				GCFrameRoots:    plans,
				GCStructHelpers: true,
				GCArrayHelpers:  true,
				Stats:           &stats,
				Codegen: codegen.Options{Module: codegen.ModuleInfo{
					GCTypeDescs: meta.Descs, GCTypeLayouts: meta.Layouts,
				}},
			})
			if cm != nil && cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := stats.Funcs[1].Calls[callKindInline]; got != tc.wantInline {
				t.Fatalf("inline calls=%d want%d (calls=%v)", got, tc.wantInline, stats.Funcs[1].Calls)
			}
			if !plan.Exact {
				t.Fatal("tiny reference class lost exact root plan")
			}
			if got := plan.CallsiteCount(); got != tc.wantCalls {
				t.Fatalf("native callsites=%d want%d", got, tc.wantCalls)
			}
		})
	}
}
