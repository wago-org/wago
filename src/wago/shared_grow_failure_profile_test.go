//go:build wago_profile && (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestSharedGrowImpossibleHelperEntries(t *testing.T) {
	const iterations = 32
	for _, address64 := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, path := range []struct {
				name string
				mode uint64
				want int
			}{{"constant-fail", 0, 0}, {"fallback", 1, iterations}, {"mixed", 2, iterations / 2}} {
				t.Run(fmt.Sprintf("i64=%t/index=%d/%s", address64, index, path.name), func(t *testing.T) {
					trace := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
					defer trace.Close()
					c, err := sharedGrowFailureConfig(t, address64).WithCodeProfile(trace).Compile(sharedGrowFailureModule(address64, index, 1, 1, 1))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					failure := ^uint64(0) - iterations + 1
					if !address64 {
						failure = uint64(uint32(failure))
					}
					if got, err := in.Invoke("loop", iterations, 1, path.mode); err != nil || len(got) != 1 || got[0] != failure {
						t.Fatalf("loop: %v, %v", got, err)
					}
					count := 0
					for _, span := range trace.Spans() {
						if span.Kind == "atomic-wait-helper" && span.Function == int(shared.AtomicHelperMemoryGrow) {
							count++
						}
					}
					t.Logf("grow helper entries = %d / %d", count, iterations)
					if count != path.want {
						t.Fatalf("grow helper entries = %d, want %d", count, path.want)
					}
				})
			}
		}
	}
}
