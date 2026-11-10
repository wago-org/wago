//go:build arm64

package hostthunk_test

import (
	"bytes"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/arm64"
	"github.com/wago-org/wago/src/core/runtime/hostthunk"
)

func TestArm64MatchesCompilerThunks(t *testing.T) {
	for _, test := range []struct {
		name string
		got  []byte
		want []byte
	}{
		{name: "indirect", got: hostthunk.Indirect(7), want: railshot.HostIndirectThunk(7)},
		{name: "sync", got: hostthunk.IndirectSync(7, 3, 2), want: railshot.HostIndirectSyncThunk(7, 3, 2)},
		{name: "owned sync", got: hostthunk.IndirectOwnedSync(7, 3, 2), want: railshot.HostIndirectOwnedSyncThunk(7, 3, 2)},
		{name: "wide parameter sync", got: hostthunk.IndirectSync(7, 65, 2), want: railshot.HostIndirectSyncThunk(7, 65, 2)},
		{name: "wide result sync", got: hostthunk.IndirectSync(7, 3, 65), want: railshot.HostIndirectSyncThunk(7, 3, 65)},
		{name: "wide parameter owned sync", got: hostthunk.IndirectOwnedSync(7, 65, 2), want: railshot.HostIndirectOwnedSyncThunk(7, 65, 2)},
		{name: "wide result owned sync", got: hostthunk.IndirectOwnedSync(7, 3, 65), want: railshot.HostIndirectOwnedSyncThunk(7, 3, 65)},
	} {
		if !bytes.Equal(test.got, test.want) {
			t.Errorf("%s runtime thunk differs from compiler thunk", test.name)
		}
	}
}

func TestArm64SyncThunkTransfersPastScaledOffsetLimit(t *testing.T) {
	for _, test := range []struct {
		name                    string
		params, results         int
		moreParams, moreResults int
	}{
		{name: "parameter", params: 4096, moreParams: 4097},
		{name: "result", results: 4096, moreResults: 4097},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := countArm64Transfers(hostthunk.IndirectSync(7, test.params, test.results))
			after := countArm64Transfers(hostthunk.IndirectSync(7, test.moreParams, test.moreResults))
			if after-before != 2 {
				t.Fatalf("one additional slot emitted %d additional memory transfers, want 2", after-before)
			}
		})
	}
}

func countArm64Transfers(code []byte) int {
	n := 0
	for i := 0; i+4 <= len(code); i += 4 {
		word := uint32(code[i]) | uint32(code[i+1])<<8 | uint32(code[i+2])<<16 | uint32(code[i+3])<<24
		switch word & 0xffc00000 {
		case 0xf9400000, 0xf9000000: // LDR/STR Xt with a scaled unsigned offset.
			n++
		}
	}
	return n
}
