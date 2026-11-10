//go:build wago_profile && ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestSharedGrowHighDeltaHelperEntries(t *testing.T) {
	const iterations = 32
	for _, index := range []byte{0, 1} {
		for _, path := range []struct {
			name          string
			first, second uint64
		}{{"high", (1 << 32) + 1, (1 << 32) + 1}, {"normal-failure", 0xffffffff, 0xffffffff}, {"mixed", (1 << 32) + 1, 0xffffffff}} {
			t.Run(fmt.Sprintf("index%d/%s", index, path.name), func(t *testing.T) {
				trace := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
				defer trace.Close()
				c, err := sharedGrowHighDeltaConfig().WithCodeProfile(trace).Compile(sharedGrowHighDeltaModule(index, false))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				got, err := in.Invoke("loop", iterations, path.first, path.second)
				if err != nil || len(got) != 1 || got[0] != ^uint64(0)-iterations+1 {
					t.Fatalf("loop:%x,%v", got, err)
				}
				count := 0
				for _, span := range trace.Spans() {
					if span.Kind == "atomic-wait-helper" && span.Function == int(shared.AtomicHelperMemoryGrow) {
						count++
					}
				}
				t.Logf("grow helper entries = %d / %d", count, iterations)
				if count != iterations {
					t.Fatalf("grow helpers%d,want%d", count, iterations)
				}
			})
		}
	}
}
