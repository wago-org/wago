//go:build linux && amd64 && wago_gc_direct_trusted_alloc

package wago

import (
	"encoding/binary"
	runtime "github.com/wago-org/wago/src/core/runtime"
	"testing"
	"unsafe"
)

func TestTrustedDirectAllocationGuards(t *testing.T) {
	for _, shape := range []string{"struct", "array"} {
		for _, guard := range []string{"disabled-pointers", "snapshot", "end-before-bump", "remaining-too-small", "counter-max"} {
			t.Run(shape+"/"+guard, func(t *testing.T) {
				data := directBumpScalarModule(false)
				args := []uint64{91}
				if shape == "array" {
					data = directArrayBumpModule(0x7f, 0x7f, 11, false)
					args = []uint64{91, 3, 2}
				}
				in := newDirectArrayBumpInstance(t, data, 256)
				if in.c.gcTrustedDirectVersion != 5 {
					t.Fatal("missing allocation attestation")
				}
				view := in.gc.DirectView()
				switch guard {
				case "disabled-pointers":
					view.AllocationEnabled = 0
					view.NurseryBumpAddress = 0
					view.AllocationCounter = 0
				case "snapshot":
					view.NurseryBump += 16
				case "end-before-bump":
					view.NurseryEnd = view.NurseryBump - 1
				case "remaining-too-small":
					view.NurseryEnd = view.NurseryBump + 15
				case "counter-max":
					*(*uint64)(unsafe.Pointer(view.AllocationCounter)) = ^uint64(0)
				}
				const sentinel = 0x81234567
				index := in.ctrl[runtime.HostCtrlImportIndexOffset:]
				binary.LittleEndian.PutUint32(index, sentinel)
				before := in.gc.Stats().Allocations
				got, e := in.Invoke("run", args...)
				if e != nil || len(got) != 1 || got[0] != 91 {
					t.Fatal(got, e)
				}
				if binary.LittleEndian.Uint32(index) == sentinel {
					t.Fatal("dynamic guard bypassed rooted helper")
				}
				// The existing Go helper's uint64 accounting wraps at MaxUint64. This test
				// proves native guard fallback, not a newly introduced saturation policy.
				if in.gc.Stats().Allocations != before+1 {
					t.Fatal("double or missing allocation")
				}
			})
		}
	}
}
