//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestIntegerReductionPlatformDefault(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows", "freebsd"} {
		for _, setting := range []string{"", "0", "1"} {
			want := setting == "1" || goos == "linux" && setting == ""
			if got := integerReductionLoopDefault(goos, setting); got != want {
				t.Fatal(goos, setting, got, want)
			}
		}
	}
}

func TestIntegerReductionEarlyRejectionPreservesCodeAndFrame(t *testing.T) {
	saved := integerReductionLoopEnabled
	defer func() { integerReductionLoopEnabled = saved }()
	for _, pressure := range []bool{false, true} {
		var m *wasm.Module
		if pressure {
			var err error
			m, err = wasm.DecodeModule(integerReductionOriginal)
			if err != nil {
				t.Fatal(err)
			}
			// These three immutable v128 constants occupy XMM homes. All values
			// are dropped; the integer regression oracle stays unchanged. Its
			// packet needs 14 XMM registers and must reject before frame capture.
			var prefix []byte
			for c := uint64(1); c <= 3; c++ {
				prefix = append(prefix, 0xfd, 12)
				prefix = binary.LittleEndian.AppendUint64(prefix, c)
				prefix = binary.LittleEndian.AppendUint64(prefix, 0)
				prefix = append(prefix, 0x1a)
			}
			m.Code[0].BodyBytes = append(prefix, m.Code[0].BodyBytes...)
		} else {
			m = integerReductionOldCopyFixture(t, false)
		}
		for _, features := range []shared.AMD64Features{shared.AMD64SSE41, shared.AMD64ModernBaseline} {
			for _, compact := range []bool{false, true} {
				var want []byte
				var frame int
				var required uint32
				for _, on := range []bool{false, true} {
					integerReductionLoopEnabled = on
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, CompactNative: compact, Stats: optionalTestStats(&stats)})
					if err != nil {
						t.Fatal(err)
					}
					if !on {
						want = append([]byte(nil), cm.Code...)
						required = cm.RequiredAMD64Features
						if diagnosticsEnabled {
							frame = stats.Funcs[0].FrameBytes
						}
					} else {
						if !bytes.Equal(cm.Code, want) || cm.RequiredAMD64Features != required {
							t.Fatal("rejection changed scalar code or features", pressure, features, compact, len(cm.Code), len(want))
						}
						if diagnosticsEnabled {
							if stats.Funcs[0].FrameBytes != frame || stats.Funcs[0].Peephole["integer-reduction-loop"] != 0 {
								t.Fatal("rejection changed frame or emitted SIMD", stats.Funcs[0])
							}
							if pressure && stats.Funcs[0].Peephole["integer-reduction-fp-entry-reject"] != 1 {
								t.Fatal("pressure fixture did not reject", stats.Funcs[0].Peephole)
							}
						}
					}
					if cm.CodeImage != nil {
						cm.CodeImage.Close()
					}
				}
			}
		}
	}
}

func TestIntegerReductionHighXMMExitWithSSE41(t *testing.T) {
	saved := integerReductionLoopEnabled
	integerReductionLoopEnabled = true
	defer func() { integerReductionLoopEnabled = saved }()
	m, err := wasm.DecodeModule(integerReductionOriginal)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: shared.AMD64SSE41})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	// Four accumulators and ten input/map/scratch registers put the exit
	// scratch in XMM13. PSRLDQ therefore needs the legacy REX extension bit.
	if !bytes.Contains(cm.Code, []byte{0x66, 0x41, 0x0f, 0x73, 0xdd, 8}) {
		t.Fatal("high-XMM legacy exit shift missing")
	}
	if got := runCompiledAmd64u(t, cm, 4096); got != 4156416353 {
		t.Fatal("high-XMM exit oracle", got)
	}
}
