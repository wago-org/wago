//go:build arm64 && (darwin || linux)

// arm64cost calibrates the small integer latency model used by Dragline's
// ARM64 scheduler. It executes equal-length dependent ADD and pointer-load
// chains through Wago's no-cgo native trampoline, alternating their order so
// process-wide frequency drift affects both samples symmetrically.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"slices"
	"time"
	"unsafe"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/encoder/arm64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const (
	chainLength = 64
	iterations  = 2_000_000
	rounds      = 9
)

type result struct {
	CPUModel         string  `json:"cpu_model"`
	TuningModel      string  `json:"tuning_model"`
	Iterations       int     `json:"iterations"`
	ChainLength      int     `json:"chain_length"`
	Rounds           int     `json:"rounds"`
	AddNanosPerOp    float64 `json:"add_nanos_per_op"`
	L1LoadNanosPerOp float64 `json:"l1_load_nanos_per_op"`
	LoadToAddRatio   float64 `json:"load_to_add_ratio"`
	LoadLatencyUnits uint16  `json:"load_latency_units"`
}

func main() {
	runtime.LockOSThread()
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		fail(err)
	}
	addCode, err := dependentAddCode()
	if err != nil {
		fail(err)
	}
	loadCode, err := dependentLoadCode()
	if err != nil {
		fail(err)
	}
	addMem, addEntry, err := coreruntime.MapCode(addCode)
	if err != nil {
		fail(err)
	}
	defer coreruntime.Unmap(addMem)
	loadMem, loadEntry, err := coreruntime.MapCode(loadCode)
	if err != nil {
		fail(err)
	}
	defer coreruntime.Unmap(loadMem)
	engine, err := coreruntime.NewEngine()
	if err != nil {
		fail(err)
	}
	defer engine.Close()

	cell := []uintptr{0}
	cell[0] = uintptr(unsafe.Pointer(&cell[0]))
	baseData := make([]byte, 64)
	linMem := uintptr(unsafe.Pointer(&baseData[32]))
	call := func(entry uintptr, a1, a2 uint64) time.Duration {
		start := time.Now()
		if _, err := engine.EnterPreparedInt(entry, linMem, iterations, a1, a2, 0); err != nil {
			fail(err)
		}
		return time.Since(start)
	}
	// Fault in code, stack, and the pointer cell before observation.
	call(addEntry, 1, 1)
	call(loadEntry, uint64(cell[0]), 0)
	addSamples := make([]float64, 0, rounds)
	loadSamples := make([]float64, 0, rounds)
	denominator := float64(iterations * chainLength)
	for round := 0; round < rounds; round++ {
		if round&1 == 0 {
			addSamples = append(addSamples, float64(call(addEntry, 1, 1))/denominator)
			loadSamples = append(loadSamples, float64(call(loadEntry, uint64(cell[0]), 0))/denominator)
		} else {
			loadSamples = append(loadSamples, float64(call(loadEntry, uint64(cell[0]), 0))/denominator)
			addSamples = append(addSamples, float64(call(addEntry, 1, 1))/denominator)
		}
	}
	runtime.KeepAlive(cell)
	runtime.KeepAlive(baseData)
	add, load := median(addSamples), median(loadSamples)
	ratio := load / add
	units := uint16(ratio + 0.5)
	if units == 0 {
		units = 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(result{
		CPUModel: target.CPUModel, TuningModel: target.TuningModel,
		Iterations: iterations, ChainLength: chainLength, Rounds: rounds,
		AddNanosPerOp: add, L1LoadNanosPerOp: load, LoadToAddRatio: ratio,
		LoadLatencyUnits: units,
	}); err != nil {
		fail(err)
	}
}

func dependentAddCode() ([]byte, error) {
	var a arm64.Asm
	a.MovReg64(arm64.X4, arm64.X0)
	a.MovReg64(arm64.X0, arm64.X1)
	loop := a.Len()
	for range chainLength {
		a.Add64(arm64.X0, arm64.X0, arm64.X2)
	}
	a.SubImm64(arm64.X4, arm64.X4, 1)
	branch := a.Cbnz64(arm64.X4)
	if !a.PatchBranch19(branch, loop) {
		return nil, fmt.Errorf("ADD calibration loop branch is out of range")
	}
	a.Ret()
	return a.B, nil
}

func dependentLoadCode() ([]byte, error) {
	var a arm64.Asm
	a.MovReg64(arm64.X4, arm64.X0)
	a.MovReg64(arm64.X0, arm64.X1)
	loop := a.Len()
	for range chainLength {
		if !a.Load64(arm64.X0, arm64.X0, 0) {
			return nil, fmt.Errorf("pointer load is not encodable")
		}
	}
	a.SubImm64(arm64.X4, arm64.X4, 1)
	branch := a.Cbnz64(arm64.X4)
	if !a.PatchBranch19(branch, loop) {
		return nil, fmt.Errorf("load calibration loop branch is out of range")
	}
	a.Ret()
	return a.B, nil
}

func median(values []float64) float64 {
	values = append([]float64(nil), values...)
	slices.Sort(values)
	return values[len(values)/2]
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "arm64cost:", err)
	os.Exit(1)
}
