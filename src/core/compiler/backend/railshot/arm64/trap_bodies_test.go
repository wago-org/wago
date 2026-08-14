//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestCompactSharedTrapBodiesArm64(t *testing.T) {
	const nop = uint32(0xd503201f)
	first := make([]byte, 16)
	second := make([]byte, 16)
	for off := 0; off < len(first); off += 4 {
		binary.LittleEndian.PutUint32(first[off:], nop)
		binary.LittleEndian.PutUint32(second[off:], nop)
	}
	info := sharedTrapBodyInfo{off: 4, endOff: 16}
	var cluster sharedTrapBodyCluster
	if got := cluster.share(nil, first, 0, info, nil); len(got) != 16 {
		t.Fatalf("first body length = %d, want 16", len(got))
	}
	stats := &CodegenStats{CodeBytes: 16, NativeSize: shared.NativeFunctionSizeReport{TotalBytes: 16, InternalFunctionBytes: 16}, GCCodeBytes: shared.GCNativeCodeBytes{Total: 16, TrapStub: 12}}
	got := cluster.share(first, second, 16, info, stats)
	if len(got) != 8 {
		t.Fatalf("shared body length = %d, want 8", len(got))
	}
	target, ok := branchTarget(4, binary.LittleEndian.Uint32(got[4:]))
	if !ok || 16+target != 4 {
		t.Fatalf("thunk target = local %d absolute %d, %v; want 4", target, 16+target, ok)
	}
	if stats.CodeBytes != 8 || stats.NativeSize.TotalBytes != 8 || stats.GCCodeBytes.TrapStub != 4 {
		t.Fatalf("stats not compacted: %+v", stats)
	}
}

func TestSharedTrapBodyPositionIndependentArm64(t *testing.T) {
	var body [4]byte
	binary.LittleEndian.PutUint32(body[:], 0x14000000) // B imm26
	if sharedTrapBodyPositionIndependent(body[:]) {
		t.Fatal("PC-relative branch accepted")
	}
	binary.LittleEndian.PutUint32(body[:], 0x58000000) // LDR literal
	if sharedTrapBodyPositionIndependent(body[:]) {
		t.Fatal("literal load accepted")
	}
}
