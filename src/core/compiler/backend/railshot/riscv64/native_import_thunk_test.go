//go:build linux && !tinygo

package riscv64

import (
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestPrivateNativeImportThunkAddressAdmission(t *testing.T) {
	target := nativeImportTarget{entry: 0x4000, home: 0x10000, context: 0x20000}
	ar, err := coreruntime.NewArena(3*nativeImportTransportBytes + 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	target.arguments, target.results = ar.Alloc(nativeImportTransportBytes), ar.Alloc(nativeImportTransportBytes)
	misaligned := ar.Alloc(nativeImportTransportBytes + 8)
	code, err := nativeImportThunk(target, 0x30000, 0x40000, MaxHostFunctionImports-1)
	if err != nil || len(code) == 0 || len(code)%4 != 0 {
		t.Fatal("bounded fixed-width thunk", err)
	}
	for _, slots := range []int{0, 1, 255, 256} {
		candidate := target
		candidate.argumentSlots, candidate.resultSlots = slots, slots
		if code, err := nativeImportThunk(candidate, 0x30000, 0x40000, 0); err != nil || len(code) == 0 || len(code)%4 != 0 || len(code) > 16<<10 {
			t.Fatal("bounded transport boundary", slots, len(code), err)
		}
	}
	for _, mutate := range []func(*nativeImportTarget, *uintptr, *uintptr, *uint32){
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.entry = 0 },
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.entry += 4 },
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.home = 0 },
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.home++ },
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.context = 0 },
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.context++ },
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.arguments = nil },
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.results = nil },
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.arguments = t.arguments[:8] },
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.results = t.results[:8] },
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) {
			t.arguments = misaligned[:nativeImportTransportBytes+1]
		},
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) {
			t.results = misaligned[:nativeImportTransportBytes+1]
		},
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) {
			t.arguments = misaligned[1 : nativeImportTransportBytes+1]
		},
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) {
			t.results = misaligned[1 : nativeImportTransportBytes+1]
		},
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.results = t.arguments },
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.argumentSlots = -1 },
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.argumentSlots = 257 },
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.resultSlots = -1 },
		func(t *nativeImportTarget, _, _ *uintptr, _ *uint32) { t.resultSlots = 257 },
		func(_ *nativeImportTarget, home, _ *uintptr, _ *uint32) { *home = 0 },
		func(_ *nativeImportTarget, home, _ *uintptr, _ *uint32) { *home++ },
		func(_ *nativeImportTarget, _, ctx *uintptr, _ *uint32) { *ctx = 0 },
		func(_ *nativeImportTarget, _, ctx *uintptr, _ *uint32) { *ctx++ },
		func(_ *nativeImportTarget, _, _ *uintptr, index *uint32) { *index = MaxHostFunctionImports },
	} {
		candidate, home, context, index := target, uintptr(0x30000), uintptr(0x40000), uint32(0)
		mutate(&candidate, &home, &context, &index)
		if bad, err := nativeImportThunk(candidate, home, context, index); bad != nil || err == nil {
			t.Fatal("invalid native import target emitted", candidate, home, context, index, err)
		}
	}
}
