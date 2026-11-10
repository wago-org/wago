//go:build arm64

package arm64

import (
	"testing"

	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestFullFlushRootStatsAcrossStagingPaths(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name     string
		slots    int
		source   int
		deferred bool
	}{
		{"ordinary", 2, 4, true},
		{"overlapping-root", 2, 0, false},
		{"overlapping-deferred-child", 2, 0, true},
		{"existing-wide-path", 65, 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats := &CodegenStats{}
			f := fn{a: &encoder.Asm{}, s: newStack(), stats: stats, globalCellReg: regNone}
			f.pushValue(storage{kind: stConst, typ: mtI64, cval: 53})
			f.pushValue(storage{kind: stSlot, typ: mtI32, slot: uint32(tc.source)})
			if tc.deferred {
				f.pushUnOp(opClz, mtI32)
			}
			for i := 2; i < tc.slots; i++ {
				f.pushValue(storage{kind: stConst, typ: mtI64, cval: int64(i)})
			}
			f.flush()
			wantDeferred := 0
			if tc.deferred {
				wantDeferred = 1
			}
			if stats.Flushes != 1 || stats.FlushRoots != tc.slots || stats.FlushDeferredRoots != wantDeferred {
				t.Fatalf("flush stats=(%d,%d,%d), want (1,%d,%d)", stats.Flushes, stats.FlushRoots, stats.FlushDeferredRoots, tc.slots, wantDeferred)
			}
		})
	}
}
