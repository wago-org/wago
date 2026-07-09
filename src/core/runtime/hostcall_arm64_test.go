//go:build (linux || darwin) && arm64

package runtime

import (
	"encoding/binary"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func hostCallFixture(t *testing.T, jm *JobMemory, ar *Arena) (serArgs, results, trap, ctrl []byte) {
	t.Helper()
	serArgs = ar.Alloc(16)
	results = ar.Alloc(16)
	trap = ar.Alloc(8)
	ctrl = ar.Alloc(ctrlFrameSize)
	jm.SetCustomCtx(slicePtr(ctrl))
	return
}

func stubHostRoundtrip() []byte {
	var a a64.Asm
	a.MovReg64(a64.X26, a64.X1) // linMem invariant expected by hostCallStub.
	a.MovReg64(a64.X19, a64.X3) // results pointer must survive the host round trip.
	a.MovImm64(a64.X20, 111)
	a.StpPre(a64.LR, a64.X20, a64.SP, -16)

	a.SubImm64(a64.X10, a64.X26, offCustomCtx)
	must(a.Load64(a64.X10, a64.X10, 0))
	must(a.Load32(a64.X11, a64.X0, 0))
	must(a.Store64(a64.X11, a64.X10, hcArgs))
	a.MovImm64(a64.X11, 7)
	must(a.Store32(a64.X11, a64.X10, hcImportIdx))
	a.MovImm64(a64.X11, 1)
	must(a.Store32(a64.X11, a64.X10, hcNArgs))
	must(a.Load64(a64.X16, a64.X10, hcTrampoline))
	a.Blr(a64.X16)

	a.SubImm64(a64.X10, a64.X26, offCustomCtx)
	must(a.Load64(a64.X10, a64.X10, 0))
	must(a.Load32(a64.X11, a64.X10, hcResults))
	must(a.Load32(a64.X12, a64.SP, 8))
	a.Add32(a64.X11, a64.X11, a64.X12)
	must(a.Store32(a64.X11, a64.X19, 0))
	a.LdpPost(a64.LR, a64.X20, a64.SP, 16)
	a.Ret()
	return a.B
}

func TestHostCallRoundtrip(t *testing.T) {
	eng, jm, ar := fixture(t)
	stub := stubHostRoundtrip()
	code, err := mmapExec(stub)
	if err != nil {
		t.Skipf("exec mapping denied: %v", err)
	}
	defer munmap(code)

	serArgs, results, trap, ctrl := hostCallFixture(t, jm, ar)
	binary.LittleEndian.PutUint32(serArgs, 20)

	calls := 0
	var sawImport uint32
	var sawArg uint64
	host := func(imp uint32, args, res []uint64) {
		calls++
		sawImport = imp
		sawArg = args[0]
		res[0] = args[0] * 2
	}

	if err := eng.CallWithHost(slicePtr(code), serArgs, jm.LinearMemory(), trap, results, ctrl, host); err != nil {
		t.Fatalf("CallWithHost: %v", err)
	}
	if calls != 1 {
		t.Fatalf("host fn invoked %d times, want 1", calls)
	}
	if sawImport != 7 || sawArg != 20 {
		t.Fatalf("host saw importIdx=%d arg=%d, want 7 and 20", sawImport, sawArg)
	}
	if got := binary.LittleEndian.Uint32(results); got != 151 {
		t.Fatalf("round-trip result = %d, want 151 (double(20)+111 sentinel)", got)
	}
}
