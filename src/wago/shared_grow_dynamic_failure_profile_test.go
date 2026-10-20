//go:build wago_profile && (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"testing"
)

func TestSharedGrowDynamicFailureHelperEntries(t *testing.T) {
	const iterations = 32
	for _, wide := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, path := range []struct {
				name       string
				base, mask uint64
				want       int
			}{{"proven-failure", 3, 0, 0}, {"quota-helper-failure", 1, 0, iterations}, {"mixed-failures", 1, 2, iterations / 2}} {
				t.Run(fmt.Sprintf("wide=%t/index=%d/%s", wide, index, path.name), func(t *testing.T) {
					trace := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
					defer trace.Close()
					c, err := sharedGrowDynamicFailureConfig().WithCodeProfile(trace).Compile(sharedGrowDynamicFailureModule(wide, index, false))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c, InstantiateOptions{memoryLimitPages: 1})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					want := ^uint64(0) - iterations + 1
					if !wide {
						want = uint64(uint32(want))
					}
					got, err := in.Invoke("loop", iterations, path.base, path.mask)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("loop:%x,%v", got, err)
					}
					count := 0
					for _, span := range trace.Spans() {
						if span.Kind == "atomic-wait-helper" && span.Function == int(shared.AtomicHelperMemoryGrow) {
							count++
						}
					}
					t.Logf("grow helper entries = %d / %d", count, iterations)
					if count != path.want {
						t.Fatalf("grow helper entries=%d,want%d", count, path.want)
					}
				})
			}
		}
	}
}

func TestSharedGrowDynamicFailureLargeBoundHelperEntries(t *testing.T) {
	for _, index := range []byte{0, 1} {
		t.Run(fmt.Sprintf("index=%d", index), func(t *testing.T) {
			trace := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
			t.Cleanup(func() { trace.Close() })
			in := sharedGrowDynamicFailureLargeConsumer(t, index, sharedGrowDynamicFailureConfig().WithCodeProfile(trace))
			count := func() int {
				n := 0
				for _, span := range trace.Spans() {
					if span.Kind == "atomic-wait-helper" && span.Function == int(shared.AtomicHelperMemoryGrow) {
						n++
					}
				}
				return n
			}
			for _, step := range []struct {
				delta uint64
				want  int
			}{{(1 << 40) + 1, 0}, {1 << 40, 1}, {1 << 39, 1}, {0, 1}, {1, 1}} {
				before := count()
				if _, err := in.Invoke("run", step.delta, 9); err != nil {
					t.Fatal(err)
				}
				got := count() - before
				t.Logf("delta%x helper entries=%d", step.delta, got)
				if got != step.want {
					t.Errorf("delta%x helper entries=%d,want%d", step.delta, got, step.want)
				}
			}
		})
	}
}
