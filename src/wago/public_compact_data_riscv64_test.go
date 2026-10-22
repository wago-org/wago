//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicCompactDataAdmission(t *testing.T) {
	c, err := Compile(nil, growMemModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	base := c.executionView()
	check := func(name string, data *compactActiveData, accepted bool) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			view := rv64ReferenceStartCopy(base)
			view.Data = nil
			view.compactData = data
			err := railshotValidateCompiled(view)
			if accepted && err != nil || !accepted && !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("compact data admission", accepted, err)
			}
		})
	}
	check("empty", &compactActiveData{}, true)
	check("changed-segment-count", &compactActiveData{records: make([]compactDataRecord, 256)}, false)
	// A hostile packed span must be rejected before any payload slice is formed.
	badSpan := make([]compactDataRecord, 257)
	badSpan[0].end = ^uint32(0)
	check("invalid-span", &compactActiveData{records: badSpan}, false)
	check("wrong-memory", &compactActiveData{records: []compactDataRecord{{memoryIndex: 1}}}, false)
	check("payload-end", &compactActiveData{records: []compactDataRecord{{end: 2}}, bytes: []byte{1}}, false)
	check("reversed-span", &compactActiveData{records: []compactDataRecord{{start: 1}}, bytes: []byte{1}}, false)
	for _, count := range []int{minCompactActiveData, minCompactActiveData + 1} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("fresh-%d-artifact%t", count, artifact), func(t *testing.T) {
				segments := make([][]byte, count)
				for i := range segments {
					segments[i] = []byte{0, 0x41, 0, 0x0b, 1, 0x5a}
				}
				source := wasmtest.Module(wasmtest.Section(5, []byte{1, 0, 1}), wasmtest.Section(11, wasmtest.Vec(segments...)))
				compiled := rv64PublicStartCompile(t, source, artifact)
				view := compiled.executionView()
				if view.compactData == nil || view.activeDataCount() != count || !rv64ArtifactContractValid(view) {
					t.Fatal("compiled compact data contract/count", view.activeDataCount())
				}
				instance, err := Instantiate(compiled)
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				if got := instance.Memory().UnsafeBytes()[0]; got != 0x5a {
					t.Fatalf("compact active data initialization = %#x", got)
				}
			})
		}
	}
}
