//go:build linux && amd64 && wago_sumunroll

package amd64

import (
	"bytes"
	"os"
	"testing"

	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestSumUnrollCapacityBound(t *testing.T) {
	m := sumUnrollModule(t, 0)
	hints := []funcHints{{flags: hintHasLoop | hintTouchesMemory}}
	used := 0
	for range 100 {
		used += sumUnrollCodeHeadroom(m, hints, 0, used)
	}
	if used != 1024 {
		t.Fatalf("reservation is not bounded: %d", used)
	}
	for _, flags := range []funcHintFlags{0, hintHasLoop, hintTouchesMemory, hintHasLoop | hintTouchesMemory | hintHasCall, hintHasLoop | hintTouchesMemory | hintHasTailCall, hintHasLoop | hintTouchesMemory | hintHasSIMD, hintHasLoop | hintTouchesMemory | hintUsesBulkMem} {
		hints[0].flags = flags
		if sumUnrollCodeHeadroom(m, hints, 0, 0) != 0 {
			t.Fatalf("reserved excluded shape: %x", flags)
		}
	}
	hints[0].flags = hintHasLoop | hintTouchesMemory
	m.Code[0].BodyBytes = make([]byte, 2049)
	if sumUnrollCodeHeadroom(m, hints, 0, 0) != 0 {
		t.Fatal("reserved large body")
	}
	saved := sumUnrollExperiment
	defer func() { sumUnrollExperiment = saved }()
	sumUnrollExperiment.factor = 16
	sumUnrollExperiment.chains = 4
	sumUnrollExperiment.reserve = true
	sumUnrollExperiment.hybrid = false
	sumUnrollExperiment.threshold = 0
	if !sumUnrollReserveEnabled(true, 1, false) {
		t.Fatal("reservation disabled")
	}
	if sumUnrollReserveEnabled(false, 1, false) || sumUnrollReserveEnabled(true, 2, false) || sumUnrollReserveEnabled(true, 1, true) {
		t.Fatal("reservation escaped narrow compile mode")
	}
	sumUnrollExperiment.factor = 0
	if sumUnrollReserveEnabled(true, 1, false) {
		t.Fatal("baseline reservation enabled")
	}
}

func TestSumUnrollCapacityPreservesCodeAndExecution(t *testing.T) {
	v := os.Getenv("WAGO_SUM_VARIANT")
	if v != "DR" && v != "PR" {
		t.Skip("reservation candidate only")
	}
	selectSumUnroll(t)
	m := sumUnrollModule(t, 0)
	m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
	m.Code = append(m.Code, m.Code[0])
	opts := CompileOptions{Workers: 1, DeferCodeMapping: true}
	on, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	sumUnrollExperiment.reserve = false
	off, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(on.Code, off.Code) {
		t.Fatal("reservation changed native bytes")
	}
	assertCompiledModuleEqual(t, on, off)
	sumUnrollExperiment.reserve = true
	native := sumUnrollNative(t, m, opts)
	mem, err := rt.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()
	data := mem.CurrentBytes()
	for i := range data {
		data[i] = byte(i*31 + 5)
	}
	for _, count := range []uint32{0, 1, 2, 3, 4, 15, 16, 17, 31, 32, 33, 512} {
		want, trap := sumOracle(data, 1, count, ^uint64(0)-9, 0)
		got, err := native.call(mem, 1, count, ^uint64(0)-9, 0)
		if trap || err != nil || got != want {
			t.Fatalf("count=%d got=%v want=%v err=%v", count, got, want, err)
		}
	}
}

func TestSumUnrollCapacityPreservesPressureCallback(t *testing.T) {
	selectSumUnroll(t)
	if !sumUnrollExperiment.reserve {
		t.Skip("reservation candidate only")
	}
	m := sumUnrollModule(t, 0)
	m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
	m.Code = append(m.Code, m.Code[0])
	for _, threshold := range []int{0, 1, 1024} {
		calls := [2]int{}
		var codes [2][]byte
		for i, enabled := range []bool{false, true} {
			sumUnrollExperiment.reserve = enabled
			cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, DeferCodeMapping: true, MemoryPressureAt: threshold, MemoryPressure: func() { calls[i]++ }})
			if err != nil {
				t.Fatal(err)
			}
			codes[i] = cm.Code
		}
		if calls[0] != calls[1] || !bytes.Equal(codes[0], codes[1]) {
			t.Fatalf("threshold=%d callback counts=%v", threshold, calls)
		}
	}
}
