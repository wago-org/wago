//go:build amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestSharedTrapBodyClusterAMD64(t *testing.T) {
	first := make([]byte, 24)
	second := make([]byte, 24)
	for i := range first {
		first[i], second[i] = byte(i+1), byte(i+1)
	}
	info := sharedTrapBodyInfoAMD64{off: 4, endOff: 24}
	var cluster sharedTrapBodyClusterAMD64
	if got := cluster.share(nil, first, 0, info, nil); len(got) != 24 {
		t.Fatalf("first body length = %d, want 24", len(got))
	}
	stats := &CodegenStats{CodeBytes: 24, NativeSize: shared.NativeFunctionSizeReport{TotalBytes: 24, InternalFunctionBytes: 24}, GCCodeBytes: shared.GCNativeCodeBytes{Total: 24, TrapStub: 20}}
	got := cluster.share(first, second, 24, info, stats)
	if len(got) != 9 || got[4] != 0xe9 {
		t.Fatalf("shared body = %d bytes, opcode %#x; want 9 and JMP", len(got), got[4])
	}
	delta := int32(binary.LittleEndian.Uint32(got[5:]))
	if target := 24 + 4 + 5 + int(delta); target != 4 {
		t.Fatalf("thunk target = %d, want 4", target)
	}
	if stats.CodeBytes != 9 || stats.NativeSize.TotalBytes != 9 || stats.GCCodeBytes.TrapStub != 5 {
		t.Fatalf("stats not compacted: %+v", stats)
	}
}

func TestSharedTrapBodyClusterRejectsTrailingLiteralPoolAMD64(t *testing.T) {
	var cluster sharedTrapBodyClusterAMD64
	code := make([]byte, 32)
	info := sharedTrapBodyInfoAMD64{off: 4, endOff: 24}
	if got := cluster.share(nil, code, 0, info, nil); len(got) != len(code) || cluster.n != 0 {
		t.Fatalf("non-tail body admitted: len=%d shapes=%d", len(got), cluster.n)
	}
}
