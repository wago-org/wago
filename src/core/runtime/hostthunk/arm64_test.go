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
	} {
		if !bytes.Equal(test.got, test.want) {
			t.Errorf("%s runtime thunk differs from compiler thunk", test.name)
		}
	}
}
