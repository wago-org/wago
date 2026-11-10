//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func swarHostOwnerRV64(t *testing.T, image *swarModuleImage, guest, thunks *coreruntime.CodeBuffer, job *coreruntime.JobMemory) *swarHostOwner {
	t.Helper()
	owner, err := image.instantiateHosts(job, guest, thunks)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	return owner
}

func TestSWARRV64HostModuleOwnerSharedSealedCodePrivateDispatchAndSurvivor(t *testing.T) {
	for _, count := range []int{1, 65, 256} {
		for _, shape := range []struct {
			slots  int
			nested bool
		}{{0, false}, {4, false}, {65, true}, {126, false}} {
			t.Run(fmt.Sprintf("imports%d/slots%d/nested%t", count, shape.slots, shape.nested), func(t *testing.T) {
				source, _, types := swarHostSource(count, count-1, shape.slots, shape.nested)
				image, err := compileSWARHostModule(source, emissionMode{production: true, interruptible: true})
				if err != nil {
					t.Fatal(err)
				}
				guest, thunks := swarHostCodeBuffers(t, image)
				e1, j1, a1, r1, t1, c1 := thunkFixtureRV64(t, shape.slots)
				e2, j2, a2, r2, t2, c2 := thunkFixtureRV64(t, shape.slots)
				o1 := swarHostOwnerRV64(t, image, guest, thunks, j1)
				o2 := swarHostOwnerRV64(t, image, guest, thunks, j2)
				entry := guest.Base() + uintptr(image.functions[0].Entry)
				identity1 := binary.LittleEndian.Uint64(swarTableOwnedBytes(o1.directory, coreruntime.FuncRefDescBytes)[coreruntime.FuncRefContextOffset:])
				identity2 := binary.LittleEndian.Uint64(swarTableOwnedBytes(o2.directory, coreruntime.FuncRefDescBytes)[coreruntime.FuncRefContextOffset:])
				if identity1 == 0 || identity2 == 0 || identity1 == identity2 || guest.Base() == 0 || thunks.Base() == 0 {
					t.Fatal("sealed sharing with private contexts")
				}
				input := [3]uint64{0x7ff0000000000001, 0x80000000deadbeef, 0xfff0000000000001}
				write := func(args, results []byte) {
					for i, value := range input {
						binary.LittleEndian.PutUint64(args[i*8:], value)
					}
					for i := range types {
						binary.LittleEndian.PutUint64(args[24+i*8:], 0xdeadbeef80000000|uint64(i))
					}
					for i := range results {
						results[i] = 0xa5
					}
				}
				expectedCtrl, calls := uintptr(0), 0
				callback := func(ctrl uintptr, index uint32, args, results []uint64) {
					if ctrl != expectedCtrl || index != uint32(count-1) || len(args) != shape.slots || len(results) != shape.slots {
						panic("owned module host metadata")
					}
					calls++
					for i, value := range args {
						want := uint64(0xdeadbeef80000000) | uint64(i)
						if types[i] == 0x7f || types[i] == 0x7d {
							want = uint64(uint32(want))
						}
						if value != want || results[i] != 0 {
							panic("owned module typed host transport")
						}
						results[i] = 0xfedcba9880000000 | uint64(i+1)
					}
				}
				run := func(engine *coreruntime.Engine, job *coreruntime.JobMemory, args, results, trap, ctrl []byte) {
					write(args, results)
					expectedCtrl, calls = thunkPointerRV64(ctrl), 0
					if err := engine.CallWithHostBase(entry, args, job.LinMemBase(), trap, results, ctrl, callback); err != nil || calls != 1 {
						t.Fatal(err)
					}
					prefix := 3
					if shape.nested {
						prefix = 6
					}
					for i := range prefix {
						if binary.LittleEndian.Uint64(results[i*8:]) != input[i%3] {
							t.Fatal("owned module raw prefix", i)
						}
					}
					for i := range types {
						want := uint64(0xfedcba9880000000) | uint64(i+1)
						if types[i] == 0x7f || types[i] == 0x7d {
							want = uint64(uint32(want))
						}
						if binary.LittleEndian.Uint64(results[(prefix+i)*8:]) != want {
							t.Fatal("owned module normalized result", i)
						}
					}
					if !bytes.Equal(results[(prefix+shape.slots)*8:], bytes.Repeat([]byte{0xa5}, len(results)-(prefix+shape.slots)*8)) {
						t.Fatal("owned module result tail")
					}
				}
				run(e1, j1, a1, r1, t1, c1)
				run(e2, j2, a2, r2, t2, c2)
				guestBase, thunkBase := guest.Base(), thunks.Base()
				if err := o1.Close(); err != nil || guest.Base() != guestBase || thunks.Base() != thunkBase || j1.CaptureInstanceContext().ImportDispatch != 0 || j1.CaptureInstanceContext().FuncRefDescPtr != 0 {
					t.Fatal("host first owner closed external images", err)
				}
				run(e2, j2, a2, r2, t2, c2)
				o1 = swarHostOwnerRV64(t, image, guest, thunks, j1)
				run(e1, j1, a1, r1, t1, c1)
				if n := testing.AllocsPerRun(50, func() {
					write(a1, r1)
					expectedCtrl, calls = thunkPointerRV64(c1), 0
					if err := e1.CallWithHostBase(entry, a1, j1.LinMemBase(), t1, r1, c1, callback); err != nil {
						panic(err)
					}
					if calls != 1 {
						panic("owned warm host call count")
					}
				}); n != 0 {
					t.Fatal("owned module scalar host warm allocations", n)
				}
			})
		}
	}
}

func TestSWARRV64HostModuleOwnerForeignDispatchGuardAndFreshGC(t *testing.T) {
	source, fixtures, _ := swarHostSource(256, 255, 65, false)
	image, err := compileSWARHostModule(source, emissionMode{production: true, interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	guest, thunks := swarHostCodeBuffers(t, image)
	_, j1, _, _, _, _ := thunkFixtureRV64(t, 65)
	e2, j2, args, results, trap, ctrl := thunkFixtureRV64(t, 65)
	o1 := swarHostOwnerRV64(t, image, guest, thunks, j1)
	o2 := swarHostOwnerRV64(t, image, guest, thunks, j2)
	entry := guest.Base() + uintptr(image.functions[0].Entry)
	row1 := swarTableOwnedBytes(o1.dispatch, 256*32)[255*32:]
	row2 := swarTableOwnedBytes(o2.dispatch, 256*32)[255*32:]
	old := bytes.Clone(row2)
	for _, forgedHome := range []bool{false, true} {
		copy(row2, row1)
		if forgedHome {
			copy(row2[8:16], old[8:16])
		}
		for i := range args {
			args[i] = byte(i*37+i/8*19) ^ 0x5a
		}
		for i := range results {
			results[i] = 0xa5
		}
		before := bytes.Clone(args)
		err := e2.CallWithHostBase(entry, args, j2.LinMemBase(), trap, results, ctrl, func(uintptr, uint32, []uint64, []uint64) { panic("foreign private host dispatch called") })
		var tr *coreruntime.TrapError
		pc := uint32(len(fixtures[0].locals) + bytes.Index(fixtures[0].body, []byte{0x10, 0xff, 1}))
		if !errors.As(err, &tr) || tr.Code != coreruntime.TrapCalledFnNotLinked || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|257 || !bytes.Equal(args, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("foreign owned host home/context guard prewrite/source", forgedHome, err)
		}
		clear(trap)
	}
	copy(row2, old)
	if err := o1.Close(); err != nil {
		t.Fatal(err)
	}
	input := [3]uint64{0x7ff0000000000001, 0x80000000deadbeef, 0xfff0000000000001}
	for i, value := range input {
		binary.LittleEndian.PutUint64(args[i*8:], value)
	}
	calls := 0
	if err := e2.CallWithHostBase(entry, args, j2.LinMemBase(), trap, results, ctrl, func(_ uintptr, index uint32, in, out []uint64) {
		goruntime.GC()
		if index != 255 || len(in) != 65 || len(out) != 65 {
			panic("survivor scalar host metadata")
		}
		calls++
	}); err != nil || calls != 1 {
		t.Fatal("surviving owner callback after other context release", calls, err)
	}
	for i, value := range input {
		if binary.LittleEndian.Uint64(results[i*8:]) != value {
			t.Fatal("owned host GC raw live carrier", i)
		}
	}
}
