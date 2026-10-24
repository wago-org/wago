//go:build amd64 && tinygo && wago_lean

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Keep optional, compiler-only GC read optimizations out of the lean TinyGo
// runtime. Their ordinary lowering paths remain available and preserve the
// same WebAssembly behavior without carrying the planning machinery.
var directForwarderEnabled = true

const (
	directForwarderAvailable = false
	gcReadRegionsAvailable   = false
	gcTestFieldAvailable     = false
	gcValueChecksAvailable   = false
)

func directForwarderTarget(*wasm.Module, int, uint32) (uint32, bool) { return 0, false }
func equalForwarderTypes([]wasm.ValType, []wasm.ValType) bool        { return false }

type gcReadRegionPlan struct {
	cacheHome bool
	params    []wasm.ValType
	result    wasm.ValType
}

type gcReadRegionState struct {
	plan  *gcReadRegionPlan
	base  int
	lease Reg
	err   error
}

func addGCReadRegionTargets(*wasm.Module, *inlineTargetTable, []codegen.GCTypeLayout, bool, CodegenPolicy) {
}

func (f *fn) gcReadRegionViolation(string) error                { return nil }
func (f *fn) prepareGCReadRegionControl(byte) error             { return nil }
func (f *fn) emitGCReadRegionGroup(*wasm.Reader) error          { return nil }
func (f *fn) emitGCReadRegionNumeric(*wasm.Reader) error        { return nil }
func (f *fn) gcReadRegionRegister() Reg                         { return regNone }
func (f *fn) stageGCReadRegionOperands(int) error               { return nil }
func (f *fn) tryGCTestField(uint32, *wasm.Reader) (bool, error) { return false, nil }

type gcValueChecks struct {
	locals    []uint32
	producers map[uint32]bool
}

func (s *gcValueChecks) nextID() uint32                           { return 0 }
func (s *gcValueChecks) setLocalValue(int, uint32)                {}
func (s *gcValueChecks) fieldValue(uint32, uint32, uint32) uint32 { return 0 }
func (f *fn) gcLocalValueID(int) uint32                           { return 0 }
func (f *fn) gcValueID(*elem) uint32                              { return 0 }
func (f *fn) setGCValueID(*elem, uint32)                          {}
func (f *fn) clearGCValueChecks()                                 {}
func (f *fn) prepareGCValueChecks(byte)                           {}
func (f *fn) prepareGCValueFB(uint32)                             {}
func (f *fn) reserveGCValueChecks([]byte, uint32) error           { return nil }
func (f *fn) reuseGCValueCheck(uint32, uint32, uint32) bool       { return false }
func (f *fn) recordGCValueCheck(uint32, uint32, uint32, Reg, Reg) {}
func emitGCValueLimit(*fn, Reg, Reg, Reg)                         {}
