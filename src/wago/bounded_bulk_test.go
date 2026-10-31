package wago

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func boundedBulkFixture(repeats uint32) []byte {
	kernel := []byte{0x41, 0, 0x41, 7, 0x20, 0, 0xfc, 11, 0, 0x41, 0, 0x2d, 0, 0, 0x20, 0, 0x41, 1, 0x6b, 0x2d, 0, 0, 0x6a, 0x0b}
	caller := []byte{1, 2, 0x7f, 0x03, 0x40, 0x20, 2, 0x20, 0, 0x10, 0, 0x6a, 0x21, 2, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 1, 0x41}
	caller = append(caller, wasmtest.SLEB32(int32(repeats))...)
	caller = append(caller, 0x49, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))), wasmtest.Section(5, wasmtest.Vec([]byte{1, 2, 0x80, 2})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("performance", 0, 1), wasmtest.ExportEntry("kernel", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(kernel), append(wasmtest.ULEB(uint32(len(caller))), caller...))))
}

func TestBoundedBulkArgumentProof(t *testing.T) {
	for _, repeats := range []uint32{1, 2, 7, 16, 64, 128} {
		m, err := wasm.DecodeModule(boundedBulkFixture(repeats))
		if err != nil {
			t.Fatal(err)
		}
		limits := boundedBulkArgumentPlans(m)
		if len(limits) != 2 || limits[0] != boundedBulkBytes<<32|1 || limits[1] != (boundedBulkBytes/uint64(repeats))<<32|1 {
			t.Fatalf("repeats=%d limits=%v", repeats, limits)
		}
	}
}

func bulkProofCaller(initial []byte, step, bound uint32, extra []byte) []byte {
	body := append([]byte(nil), initial...)
	body = append(body, 0x21, 1, 0x03, 0x40, 0x20, 2, 0x20, 0, 0x10, 0, 0x6a, 0x21, 2)
	body = append(body, extra...)
	body = append(body, 0x20, 1, 0x41)
	body = append(body, wasmtest.SLEB32(int32(step))...)
	body = append(body, 0x6a, 0x21, 1, 0x20, 1, 0x41)
	body = append(body, wasmtest.SLEB32(int32(bound))...)
	body = append(body, 0x49, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
	return body
}
func bulkProofConstant(v uint32) []byte { return append([]byte{0x41}, wasmtest.SLEB32(int32(v))...) }
func bulkPlanAt(plans []uint64, i int) uint64 {
	if i >= len(plans) {
		return 0
	}
	return plans[i]
}

func TestBoundedBulkCounterProof(t *testing.T) {
	for _, tc := range []struct {
		start, step, bound, iterations uint32
		ok                             bool
	}{
		{0, 1, 0, 1, true}, {0, 1, 64, 64, true}, {3, 2, 17, 7, true}, {17, 3, 2, 1, true},
		{0, 0, 64, 0, false}, {0, 1, 0xffffffff, 0, false}, {0xfffffffd, 2, 0xffffffff, 1, true}, {0xfffffffc, 2, 0xffffffff, 0, false},
		{0, 0xffffffff, 64, 1, true}, {0, 1, 1024, 0, false},
	} {
		m, err := wasm.DecodeModule(boundedBulkFixture(1))
		if err != nil {
			t.Fatal(err)
		}
		m.Code[1].BodyBytes = bulkProofCaller(bulkProofConstant(tc.start), tc.step, tc.bound, nil)
		got := bulkPlanAt(boundedBulkArgumentPlans(m), 1)
		want := uint64(0)
		if tc.ok {
			want = (boundedBulkBytes/uint64(tc.iterations))<<32 | 1
		}
		if got != want {
			t.Fatalf("start=%d step=%d bound=%d plan=%x want=%x", tc.start, tc.step, tc.bound, got, want)
		}
	}
}

func TestBoundedBulkRejectsUnknownWork(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*wasm.Module)
	}{
		{"counter-write", func(m *wasm.Module) {
			m.Code[1].BodyBytes = bulkProofCaller(bulkProofConstant(0), 1, 64, []byte{0x41, 0, 0x21, 1})
		}},
		{"counter-tee", func(m *wasm.Module) {
			m.Code[1].BodyBytes = bulkProofCaller(bulkProofConstant(0), 1, 64, []byte{0x41, 0, 0x22, 1, 0x1a})
		}},
		{"parameter-counter", func(m *wasm.Module) { m.Code[1].BodyBytes = bulkProofCaller([]byte{0x20, 0}, 1, 64, nil) }},
		{"changed-count", func(m *wasm.Module) {
			m.Code[1].BodyBytes = bulkProofCaller(bulkProofConstant(0), 1, 64, []byte{0x20, 0, 0x41, 1, 0x6a, 0x21, 0})
		}},
		{"signed-latch", func(m *wasm.Module) {
			m.Code[1].BodyBytes = bytes.Replace(m.Code[1].BodyBytes, []byte{0x49, 0x0d}, []byte{0x48, 0x0d}, 1)
		}},
		{"wrong-label", func(m *wasm.Module) {
			m.Code[1].BodyBytes = bytes.Replace(m.Code[1].BodyBytes, []byte{0x0d, 0, 0x0b}, []byte{0x0d, 1, 0x0b}, 1)
		}},
		{"recursion", func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x20, 0, 0x10, 1, 0x0b} }},
		{"unknown-count", func(m *wasm.Module) {
			m.Code[0].BodyBytes = bytes.Replace(m.Code[0].BodyBytes, []byte{0x20, 0, 0xfc, 11}, []byte{0x41, 0, 0x2d, 0, 0, 0xfc, 11}, 1)
		}},
		{"grow", func(m *wasm.Module) {
			m.Code[1].BodyBytes = append([]byte{0x41, 0, 0x40, 0, 0x1a}, m.Code[1].BodyBytes...)
		}},
		{"local-type", func(m *wasm.Module) { m.Code[1].Locals.Runs[0].Type = wasm.I64 }},
		{"truncated", func(m *wasm.Module) { m.Code[1].BodyBytes = []byte{0x03, 0x40, 0x20, 0x80} }},
		{"extra-end", func(m *wasm.Module) { m.Code[1].BodyBytes = append(m.Code[1].BodyBytes, 0x0b) }},
		{"shared", func(m *wasm.Module) { m.Memories[0].Shared = true }},
		{"memory64", func(m *wasm.Module) { m.Memories[0].Limits.Addr64 = true }},
		{"imports", func(m *wasm.Module) { m.Imports = []wasm.Import{{}} }},
		{"tables", func(m *wasm.Module) { m.Tables = []wasm.Table{{}} }},
		{"globals", func(m *wasm.Module) { m.Globals = []wasm.Global{{}} }},
		{"tags", func(m *wasm.Module) { m.Tags = []wasm.TagType{{}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := wasm.DecodeModule(boundedBulkFixture(64))
			if err != nil {
				t.Fatal(err)
			}
			tc.change(m)
			if got := bulkPlanAt(boundedBulkArgumentPlans(m), 1); got != 0 {
				t.Fatalf("unsafe work admitted: %x", got)
			}
		})
	}
}

func bulkPrimitiveFixture(params, count int, copyOp bool, fixed *uint32) []byte {
	var types []wasm.ValType
	for range params {
		types = append(types, wasm.I32)
	}
	body := []byte{0x41, 0, 0x41, 7}
	if params >= 3 {
		body = []byte{0x20, 0, 0x20, 1}
	}
	if fixed != nil {
		body = append(body, bulkProofConstant(*fixed)...)
	} else {
		body = append(body, 0x20, byte(count))
	}
	sub := byte(11)
	if copyOp {
		sub = 10
	}
	body = append(body, 0xfc, sub, 0)
	if copyOp {
		body = append(body, 0)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, nil))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(5, wasmtest.Vec([]byte{1, 2, 0x80, 2})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("bulk", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestBoundedBulkParameterPlans(t *testing.T) {
	for params := 1; params <= 4; params++ {
		for arg := 0; arg < params; arg++ {
			for _, copyOp := range []bool{false, true} {
				m, err := wasm.DecodeModule(bulkPrimitiveFixture(params, arg, copyOp, nil))
				if err != nil {
					t.Fatal(err)
				}
				cost := uint64(1)
				if copyOp {
					cost = 2
				}
				want := (boundedBulkBytes/cost)<<32 | uint64(arg+1)
				if got := bulkPlanAt(boundedBulkArgumentPlans(m), 0); got != want {
					t.Fatalf("params=%d arg=%d copy=%t got=%x want=%x", params, arg, copyOp, got, want)
				}
			}
		}
	}
	for _, count := range []uint32{0, 1, 4096, uint32(boundedBulkBytes)} {
		m, err := wasm.DecodeModule(bulkPrimitiveFixture(0, 0, false, &count))
		if err != nil {
			t.Fatal(err)
		}
		if got := bulkPlanAt(boundedBulkArgumentPlans(m), 0); got != uint64(0xffffffff)<<32 {
			t.Fatalf("fixed=%d plan=%x", count, got)
		}
	}
}

func TestBoundedBulkCallDepth(t *testing.T) {
	m, err := wasm.DecodeModule(bulkPrimitiveFixture(1, 0, false, nil))
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 18; i++ {
		m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
		m.Code = append(m.Code, wasm.Func{BodyBytes: []byte{0x20, 0, 0x10, byte(i - 1), 0x0b}})
	}
	plans := boundedBulkArgumentPlans(m)
	for i := 0; i < 18; i++ {
		want := i < 16
		if got := bulkPlanAt(plans, i) != 0; got != want {
			t.Fatalf("call depth=%d admitted=%t want=%t", i+1, got, want)
		}
	}
}

func TestBoundedBulkCounterArithmetic(t *testing.T) {
	random := rand.New(rand.NewSource(1073))
	for trial := 0; trial < 5000; trial++ {
		start, step, bound := random.Uint32(), random.Uint32(), random.Uint32()
		if trial%2 == 0 {
			start = uint32(random.Intn(256))
			step = uint32(random.Intn(7) + 1)
			bound = uint32(random.Intn(1024))
		}
		m, err := wasm.DecodeModule(boundedBulkFixture(1))
		if err != nil {
			t.Fatal(err)
		}
		m.Code[1].BodyBytes = bulkProofCaller(bulkProofConstant(start), step, bound, nil)
		plan := bulkPlanAt(boundedBulkArgumentPlans(m), 1)
		if plan == 0 {
			continue
		}
		// Independently execute wrapping i32 induction instead of reusing the
		// proof's division/ceiling calculation.
		counter, iterations := start, uint64(0)
		for {
			iterations++
			counter += step
			if counter >= bound {
				break
			}
			if iterations > boundedBulkSteps {
				t.Fatalf("nonterminating admitted loop: %d/%d/%d", start, step, bound)
			}
		}
		if iterations*uint64(uint32(plan>>32)) > boundedBulkBytes {
			t.Fatalf("undercharged %d/%d/%d: iterations=%d plan=%x", start, step, bound, iterations, plan)
		}
	}
}

func TestBoundedBulkCallParameterSubstitution(t *testing.T) {
	for _, mode := range []int{0, 1, 2} {
		callee := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 11, 0, 0x0b}
		caller := []byte{0x41, 0, 0x41, 7}
		switch mode {
		case 0:
			caller = append(caller, 0x20, 0)
		case 1:
			caller = append(caller, bulkProofConstant(1024)...)
		case 2:
			caller = append(caller, 0x20, 0, 0x41, 1, 0x6a)
		}
		caller = append(caller, 0x10, 0, 0x0b)
		m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))), wasmtest.Section(5, wasmtest.Vec([]byte{1, 2, 0x80, 2})), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(callee), wasmtest.Code(caller)))))
		if err != nil {
			t.Fatal(err)
		}
		want := uint64(0)
		if mode == 0 {
			want = boundedBulkBytes<<32 | 1
		}
		if mode == 1 {
			want = uint64(0xffffffff) << 32
		}
		if got := bulkPlanAt(boundedBulkArgumentPlans(m), 1); got != want {
			t.Fatalf("mode=%d got=%x want=%x", mode, got, want)
		}
	}
	m, err := wasm.DecodeModule(bulkPrimitiveFixture(4, 2, false, nil))
	if err != nil {
		t.Fatal(err)
	}
	m.Code[0].BodyBytes = append(m.Code[0].BodyBytes[:len(m.Code[0].BodyBytes)-1], 0x20, 0, 0x20, 1, 0x20, 3, 0xfc, 11, 0, 0x0b)
	if got := bulkPlanAt(boundedBulkArgumentPlans(m), 0); got != 0 {
		t.Fatalf("independent dynamic counts admitted: %x", got)
	}
}

func FuzzBoundedBulkProof(f *testing.F) {
	m, err := wasm.DecodeModule(boundedBulkFixture(64))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(m.Code[1].BodyBytes)
	f.Add([]byte{0x03, 0x40, 0x20, 0x80})
	f.Add([]byte{0x41, 1, 0x0b})
	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > 1024 {
			t.Skip()
		}
		m, err := wasm.DecodeModule(boundedBulkFixture(64))
		if err != nil {
			t.Fatal(err)
		}
		m.Code[1].BodyBytes = body
		for _, plan := range boundedBulkArgumentPlans(m) {
			if plan != 0 && (uint32(plan) > 4 || plan>>32 == 0) {
				t.Fatalf("invalid plan %x", plan)
			}
		}
	})
}

func TestBoundedBulkNestedLoops(t *testing.T) {
	m, err := wasm.DecodeModule(boundedBulkFixture(1))
	if err != nil {
		t.Fatal(err)
	}
	m.Code[1].Locals.Runs[0].Count = 3
	body := []byte{0x03, 0x40, 0x41, 0, 0x21, 3, 0x03, 0x40, 0x20, 2, 0x20, 0, 0x10, 0, 0x6a, 0x21, 2,
		0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x20, 3, 0x41, 3, 0x49, 0x0d, 0, 0x0b,
		0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 1, 0x41, 2, 0x49, 0x0d, 0, 0x0b, 0x20, 2, 0x0b}
	m.Code[1].BodyBytes = body
	if got, want := bulkPlanAt(boundedBulkArgumentPlans(m), 1), (boundedBulkBytes/6)<<32|1; got != want {
		t.Fatalf("nested plan=%x want=%x", got, want)
	}
	// Without reinitialization, an inner induction variable is loop-carried.
	m.Code[1].BodyBytes = append(append([]byte(nil), body[:2]...), body[6:]...)
	if got := bulkPlanAt(boundedBulkArgumentPlans(m), 1); got != 0 {
		t.Fatalf("loop-carried nested counter admitted: %x", got)
	}
}

func TestBoundedBulkCallDepthIndependentOfDeclarationOrder(t *testing.T) {
	m, err := wasm.DecodeModule(bulkPrimitiveFixture(1, 0, false, nil))
	if err != nil {
		t.Fatal(err)
	}
	leaf := m.Code[0]
	m.Code = nil
	for i := 0; i < 17; i++ {
		m.Code = append(m.Code, wasm.Func{BodyBytes: []byte{0x20, 0, 0x10, byte(i + 1), 0x0b}})
		m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
	}
	m.Code = append(m.Code, leaf)
	plans := boundedBulkArgumentPlans(m)
	for i := 0; i < 18; i++ {
		want := i >= 2
		if got := bulkPlanAt(plans, i) != 0; got != want {
			t.Fatalf("function=%d depth=%d admitted=%t want=%t", i, 18-i, got, want)
		}
	}
}
