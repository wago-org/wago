//go:build linux && !tinygo

package riscv64

import (
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

const callFrameHeader = 48
const maxDirectCalls = 1024

type directCall struct {
	site   int
	callee uint32
}

// Shared callees can allocate A0..A3. Caller homes and this saved header are
// authoritative across calls; X24 is reserved and unchanged in this same-instance
// ABI. Every callee consumes the shared serialized arena before it can nest.
func (f *integerEmitter) restoreCallTransport() {
	f.a.Ld(enc.X10, enc.SP, 16)
	if f.mode.production {
		f.a.Ld(enc.X13, enc.SP, 8)
		f.a.Ld(enc.X11, enc.SP, 32)
		f.a.Ld(enc.X12, enc.SP, 24)
	} else {
		f.a.Ld(enc.X11, enc.SP, 8)
		f.a.Ld(enc.X12, enc.SP, 24)
		f.a.Ld(enc.X13, enc.SP, 32)
	}
}
func (f *integerEmitter) directCall(index uint32, sig functionSpec, base, importedFunctions int) {
	imported := index < uint32(importedFunctions)
	if imported {
		f.importCallTarget(index)
	}
	f.restoreCallTransport()
	for j, wide := range sig.params {
		f.Load(5, f.SpillOffset(base+j), wide)
		// i32 loads are already zero-normalized; fill the complete arena cell.
		f.a.Sd(enc.X5, enc.X10, int32(j*8))
	}
	if imported {
		f.a.Jalr(enc.RA, enc.X28, 0)
	} else {
		f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), callee: index - uint32(importedFunctions)})
	}
	f.restoreCallTransport()
	if !f.mode.production {
		f.a.Lwu(enc.X7, enc.X12, 0)
		f.propagate = append(f.propagate, f.a.BranchLong(enc.NE, enc.X7, enc.Zero))
	}
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	for j, wide := range sig.results {
		if wide {
			f.a.Ld(enc.X5, result, int32(j*8))
		} else {
			f.a.Lwu(enc.X5, result, int32(j*8))
		}
		f.Store(f.SpillOffset(base+j), 5, wide)
	}
}

// Resolve the current activation's dynamic host target before any call-arena
// write. Equal linear memory alone does not establish instance identity.
func (f *integerEmitter) importCallTarget(index uint32) {
	f.a.Ld(enc.X6, enc.X25, -abi.ImportDispatchPtrOffset)
	f.trap(trapImportMissing, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.a.Constant64(enc.X7, int64(index)*coreruntime.ImportDispatchEntryBytes)
	f.a.Add(enc.X6, enc.X6, enc.X7)
	f.a.Ld(enc.X28, enc.X6, coreruntime.ImportDispatchCodePtrOffset)
	f.trap(trapImportMissing, f.a.BranchLong(enc.EQ, enc.X28, enc.Zero))
	f.a.Constant32(enc.X7, 3)
	f.a.And(enc.X7, enc.X28, enc.X7)
	f.trap(trapImportMissing, f.a.BranchLong(enc.NE, enc.X7, enc.Zero))
	f.a.Ld(enc.X7, enc.X6, coreruntime.ImportDispatchHomeLinMemOffset)
	f.trap(trapImportContext, f.a.BranchLong(enc.NE, enc.X7, enc.X25))
	f.a.Ld(enc.X7, enc.X6, coreruntime.ImportDispatchTargetContextOffset)
	f.trap(trapImportContext, f.a.BranchLong(enc.EQ, enc.X7, enc.Zero))
	f.a.Ld(enc.X5, enc.X6, coreruntime.ImportDispatchCallerContextOffset)
	f.trap(trapImportContext, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
	f.a.Ld(enc.X6, enc.X25, -abi.FuncRefDescPtrOffset)
	f.trap(trapImportContext, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.a.Ld(enc.X6, enc.X6, coreruntime.FuncRefContextOffset)
	f.trap(trapImportContext, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.trap(trapImportContext, f.a.BranchLong(enc.NE, enc.X7, enc.X6))
}
