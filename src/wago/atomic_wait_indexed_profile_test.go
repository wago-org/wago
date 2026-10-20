//go:build wago_profile && (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestIndexedAtomicWaitHelperEntries(t *testing.T) {
	const iterations = 32
	for _, address64 := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, sub := range []byte{1, 2} {
				for _, path := range []struct {
					name       string
					base, mask uint64
					want       int
					result     uint64
				}{{"mismatch", 1, 0, 0, iterations}, {"matching-helper", 0, 0, iterations, 2 * iterations}, {"mixed", 0, 1, iterations / 2, 3 * iterations / 2}} {
					t.Run(fmt.Sprintf("addr64=%t/index=%d/op=%d/%s", address64, index, sub, path.name), func(t *testing.T) {
						trace := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
						defer trace.Close()
						c, err := indexedWaitConfig().WithCodeProfile(trace).Compile(indexedWaitModule(address64, index, sub, 0, false))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						in, err := Instantiate(c)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						got, err := in.Invoke("loop", iterations, 0, path.base, path.mask, 0)
						if err != nil || len(got) != 1 || got[0] != path.result {
							t.Fatalf("loop:%x,%v;want%d", got, err, path.result)
						}
						helper := int(shared.AtomicHelperWait32)
						if sub == 2 {
							helper = int(shared.AtomicHelperWait64)
						}
						count := 0
						for _, span := range trace.Spans() {
							if span.Kind == "atomic-wait-helper" && span.Function == helper {
								count++
							}
						}
						t.Logf("wait helper entries = %d / %d", count, iterations)
						if count != path.want {
							t.Fatalf("wait helper entries%d,want%d", count, path.want)
						}
					})
				}
			}
		}
	}
}
