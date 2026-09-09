//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestIntervalRegionI64ResidencyWeightPolicy(t *testing.T) {
	policy := func(on bool) CodegenPolicy {
		selection, err := optimizationBindings.ResolveSnapshot(map[string]bool{"interval-i64-weight": on}, OptimizationSnapshot{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return shared.DefaultCodegenPolicy(selection)
	}
	base := fn{intervalScore: []uint32{8, 8}, localType: []machineType{mtI32, mtI64}}
	base.policy = policy(false)
	if i32, i64 := base.intervalResidencyScore(0), base.intervalResidencyScore(1); i32 != 8 || i64 != i32 {
		t.Fatalf("equal-width scores = i32 %d i64 %d, want 8/8", i32, i64)
	}
	base.policy = policy(true)
	if i32, i64 := base.intervalResidencyScore(0), base.intervalResidencyScore(1); i32 != 8 || i64 != 12 {
		t.Fatalf("weighted scores = i32 %d i64 %d, want 8/12", i32, i64)
	}
}

func intervalRegionBody() []byte {
	body := []byte{0x01, 0x14, 0x7f} // twenty i32 locals
	for x := byte(0); x < 20; x++ {
		body = append(body, 0x41, x+1, 0x21, x) // local[x] = x+1
	}
	body = append(body, 0x20, 0x00)
	for x := byte(1); x < 20; x++ {
		body = append(body, 0x20, x, 0x6a)
	}
	body = append(body, 0x0b)
	return body
}

func intervalRegionModule(t *testing.T) *wasm.Module {
	return mod1(t, nil, []wasm.ValType{wasm.I32}, intervalRegionBody())
}

func TestIntervalRegionDynamicReuse(t *testing.T) {
	savedRegions, savedScratch, savedR8 := intervalRegionPinsEnabled, intervalScratchLeaseEnabled, intervalR8LeaseEnabled
	defer func() {
		intervalRegionPinsEnabled, intervalScratchLeaseEnabled, intervalR8LeaseEnabled = savedRegions, savedScratch, savedR8
	}()
	m := intervalRegionModule(t)

	intervalRegionPinsEnabled, intervalScratchLeaseEnabled, intervalR8LeaseEnabled = true, true, true
	on := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m); got != 210 {
		t.Fatalf("enabled result = %d, want 210", got)
	}
	if on.Peephole["interval-region"] != 1 {
		t.Fatalf("interval-region = %d, want 1 (all: %v)", on.Peephole["interval-region"], on.Peephole)
	}
	if on.Peephole["interval-region-reactivate"] == 0 {
		t.Fatalf("dynamic regional cache did not reuse a register: %v", on.Peephole)
	}
	if on.Peephole["tree-order"] != 0 {
		t.Fatalf("tree ordering must stay disabled while regional registers are active: %v", on.Peephole)
	}
	if r := on.Residency; r.Events == 0 || r.EventOverflows != 0 || r.Candidates != 20 || r.Activations == 0 || r.MaxActive == 0 || r.MaxActive > maxIntervalRegionRegs+1 || r.FinalTransfers == 0 {
		t.Fatalf("residency stats = %+v", r)
	}
	if on.Peephole["interval-scratch-lease"] != 1 {
		t.Fatalf("scratch lease = %d, want 1 (all: %v)", on.Peephole["interval-scratch-lease"], on.Peephole)
	}
	if on.Peephole["interval-r8-lease"] != 0 {
		t.Fatalf("explicit-bounds R8 lease = %d, want 0 (all: %v)", on.Peephole["interval-r8-lease"], on.Peephole)
	}
	signal := compileWithStats(t, m, true).Funcs[0]
	if signal.Peephole["interval-r8-lease"] != 1 {
		t.Fatalf("signal-bounds R8 lease = %d, want 1 (all: %v)", signal.Peephole["interval-r8-lease"], signal.Peephole)
	}
	if got, want := signal.Residency.MaxActive, maxIntervalRegionRegs+1; got != want {
		t.Fatalf("signal-bounds max-active = %d, want %d", got, want)
	}
	// Every non-accumulator local is read once, below the shadow planner's
	// two-read admission threshold.
	if p := on.Residency.Shadow; p.Candidates != 0 || p.Segments != 0 || p.FailSoft != 0 {
		t.Fatalf("residency shadow = %+v", p)
	}

	intervalRegionPinsEnabled = false
	if got := runAmd64(t, m); got != 210 {
		t.Fatalf("disabled result = %d, want 210", got)
	}
}

func TestIntervalRegionScratchLeaseRejectsDivision(t *testing.T) {
	savedRegions, savedScratch := intervalRegionPinsEnabled, intervalScratchLeaseEnabled
	defer func() {
		intervalRegionPinsEnabled, intervalScratchLeaseEnabled = savedRegions, savedScratch
	}()
	intervalRegionPinsEnabled, intervalScratchLeaseEnabled = true, true

	m := intervalRegionModule(t)
	body := m.Code[0].BodyBytes
	m.Code[0].BodyBytes = append(append([]byte(nil), body[:len(body)-1]...),
		0x41, 0x01, // i32.const 1
		0x6e, // i32.div_u
		0x0b)
	stats := compileWithStats(t, m, false).Funcs[0]
	if stats.Peephole["interval-region"] != 1 {
		t.Fatalf("regional cache not exercised: %v", stats.Peephole)
	}
	if got := stats.Peephole["interval-scratch-lease"]; got != 0 {
		t.Fatalf("division admitted fixed-scratch lease %d times: %v", got, stats.Peephole)
	}
	if got := runAmd64(t, m); got != 210 {
		t.Fatalf("division module result = %d, want 210", got)
	}
}

func TestIntervalRegionScratchLeaseRejectsSIMD(t *testing.T) {
	savedRegions, savedScratch := intervalRegionPinsEnabled, intervalScratchLeaseEnabled
	defer func() {
		intervalRegionPinsEnabled, intervalScratchLeaseEnabled = savedRegions, savedScratch
	}()
	intervalRegionPinsEnabled, intervalScratchLeaseEnabled = true, true

	regionBody := intervalRegionBody()
	simdBody := []byte{0x00, 0xfd, 0x0c} // no locals; v128.const
	simdBody = append(simdBody, make([]byte, 16)...)
	simdBody = append(simdBody, 0x1a, 0x0b) // drop; end
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: regionBody},
		funcDef{body: simdBody},
	)
	for _, workers := range []int{1, 2} {
		var moduleStats ModuleStats
		compiled, err := CompileModuleWith(m, CompileOptions{Workers: workers, Stats: &moduleStats})
		if err != nil {
			t.Fatalf("workers=%d: %v", workers, err)
		}
		if compiled.CodeImage != nil {
			compiled.CodeImage.Close()
		}
		stats := moduleStats.Funcs[0]
		if got := stats.Peephole["interval-scratch-lease"]; got != 0 {
			t.Fatalf("workers=%d: SIMD-module scalar helper scratch leases = %d, want 0: %v", workers, got, stats.Peephole)
		}
	}
	if got := runAmd64(t, m); got != 210 {
		t.Fatalf("SIMD module result = %d, want 210", got)
	}
}

func TestMemSizeRegionalLeasePolicy(t *testing.T) {
	m := intervalRegionModule(t)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	body := m.Code[0].BodyBytes
	// Exercise an explicit bounds check without changing the function result.
	m.Code[0].BodyBytes = append([]byte{0x41, 0x00, 0x28, 0x02, 0x00, 0x1a}, body...)

	compile := func(on bool) CodegenStats {
		var ms ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{
			Workers:       1,
			Stats:         &ms,
			Optimizations: map[string]bool{"memsize-regional-lease": on},
		})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		return *ms.Funcs[0]
	}

	on := compile(true)
	if got := on.Peephole["memsize-regional-lease"]; got != 1 {
		t.Fatalf("enabled lease = %d, want 1: %v", got, on.Peephole)
	}
	if got := on.Residency.MaxActive; got != maxIntervalRegionRegs+1 {
		t.Fatalf("enabled max-active = %d, want %d", got, maxIntervalRegionRegs+1)
	}
	off := compile(false)
	if got := off.Peephole["memsize-regional-lease"]; got != 0 {
		t.Fatalf("disabled lease = %d, want 0: %v", got, off.Peephole)
	}
	if got := off.Residency.MaxActive; got > maxIntervalRegionRegs {
		t.Fatalf("disabled max-active = %d, want <= %d", got, maxIntervalRegionRegs)
	}
}

func TestSparseGlobalHintContains(t *testing.T) {
	hints := []shared.GlobalHint{{Index: 2}, {Index: 9}}
	if !sparseGlobalHintContains(hints, 2) || !sparseGlobalHintContains(hints, 9) {
		t.Fatal("referenced module global not found")
	}
	if sparseGlobalHintContains(hints, 3) {
		t.Fatal("unreferenced module global reported as referenced")
	}
}

func TestSelectModuleGlobalRegionalLease(t *testing.T) {
	pins := []moduleGlobalPin{{global: 2, reg: R14}, {global: 9, reg: R13}}
	flags := hintIntervalRegionStorage
	if got := selectModuleGlobalRegionalLease(true, true, true, true, false, 0, flags, pins, []shared.GlobalHint{{Index: 2}}); got != R13 {
		t.Fatalf("lease = %v, want unused R13", got)
	}
	if got := selectModuleGlobalRegionalLease(false, true, true, true, false, 0, flags, pins, nil); got != regNone {
		t.Fatalf("disabled lease = %v, want none", got)
	}
	if got := selectModuleGlobalRegionalLease(true, true, true, true, false, 0, flags, pins, []shared.GlobalHint{{Index: 2}, {Index: 9}}); got != regNone {
		t.Fatalf("all-referenced lease = %v, want none", got)
	}
	for _, reject := range []struct {
		name  string
		call  bool
		flags funcHintFlags
	}{
		{name: "call", call: true, flags: flags},
		{name: "control", flags: flags | hintHasControlFlow},
		{name: "bulk", flags: flags | hintUsesBulkMem},
	} {
		if got := selectModuleGlobalRegionalLease(true, true, true, true, reject.call, 0, reject.flags, pins, nil); got != regNone {
			t.Errorf("%s lease = %v, want none", reject.name, got)
		}
	}
}

func TestIntervalRegionRegisterPolicy(t *testing.T) {
	if len(intervalRegionOrder) < maxIntervalRegionRegs {
		t.Fatalf("regional order has %d registers for limit %d", len(intervalRegionOrder), maxIntervalRegionRegs)
	}
	if got := intervalRegionOrder[len(intervalRegionOrder)-1]; got != R8 {
		t.Fatalf("last-choice regional register = %v, want R8", got)
	}
	var seen regMask
	for _, reg := range intervalRegionOrder {
		if reg == RAX || reg == RCX || reg == RDX {
			t.Fatalf("fixed arithmetic register %v is regionally leased", reg)
		}
		if seen.has(reg) {
			t.Fatalf("regional register %v appears more than once", reg)
		}
		seen = seen.add(reg)
	}
}

func TestIntervalRegionRegisterLimitByBoundsMode(t *testing.T) {
	if got, want := intervalRegionRegLimit(false), maxIntervalRegionRegs; got != want {
		t.Fatalf("explicit-bounds register limit = %d, want %d", got, want)
	}
	if got, want := intervalRegionRegLimit(true), maxIntervalRegionRegs-1; got != want {
		t.Fatalf("signals-bounds register limit = %d, want %d", got, want)
	}
}

func TestIntervalRegionLastGetStorageOnlyForCandidates(t *testing.T) {
	saved := intervalRegionPinsEnabled
	defer func() { intervalRegionPinsEnabled = saved }()
	intervalRegionPinsEnabled = true

	m := intervalRegionModule(t)
	longBody := make([]byte, minIntervalRegionBody+1)
	for i := range longBody[:len(longBody)-4] {
		longBody[i] = 0x01 // nop
	}
	copy(longBody[len(longBody)-4:], []byte{0x20, 0x00, 0x1a, 0x0b}) // local.get 0; drop; end
	m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0], m.FuncTypes[0])
	m.Code = append(m.Code, wasm.Func{
		Locals:    wasm.Locals{Runs: []wasm.LocalRun{{Count: 100, Type: wasm.I32}}},
		BodyBytes: []byte{0x0b},
	}, wasm.Func{
		Locals:    wasm.Locals{Runs: []wasm.LocalRun{{Count: 100, Type: wasm.I32}}},
		BodyBytes: longBody,
	})
	hints, sidecar, _, err := computeModuleHints(m, 0, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(sidecar.view(hints[0]).localLastGet); got != 20 {
		t.Fatalf("candidate last-get storage = %d locals, want 20", got)
	}
	if got := sidecar.view(hints[0]).localEventCount(); got == 0 {
		t.Fatal("candidate did not retain an event summary")
	}
	ineligible := sidecar.view(hints[1])
	if got, want := ineligible.nLocals, 100; got != want {
		t.Fatalf("ineligible local count = %d, want %d", got, want)
	}
	if got, want := len(ineligible.localScore), 64; got != want {
		t.Fatalf("ineligible retained scores = %d, want %d", got, want)
	}
	if got := ineligible.localLastGet; got != nil {
		t.Fatalf("ineligible function reserved %d last-get entries", len(got))
	}
	if got := ineligible.localEventCount(); got != 0 {
		t.Fatalf("ineligible function retained %d events", got)
	}
	eligible := sidecar.view(hints[2])
	if got, want := len(eligible.localScore), 100; got != want {
		t.Fatalf("wide candidate retained scores = %d, want %d", got, want)
	}
	if got, want := len(eligible.localLastGet), 100; got != want {
		t.Fatalf("wide candidate last-get storage = %d, want %d", got, want)
	}
}
