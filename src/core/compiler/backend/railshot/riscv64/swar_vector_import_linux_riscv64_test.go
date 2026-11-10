//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Public native edges bypass these private templates. Execute the actual
// template image independently so logical-vector counts cannot silently replace
// physical-cell counts on either side, including the fixed/wide frame boundary.
func TestSWARRV64VectorImportTemplatePhysicalArgumentsAndResults(t *testing.T) {
	for _, params := range []int{0, 32, 33, 128} {
		for _, results := range []int{0, 32, 33, 128} {
			t.Run(fmt.Sprintf("params%d/results%d", params, results), func(t *testing.T) {
				vectorTypes := func(n int) []wasm.ValType {
					values := make([]wasm.ValType, n)
					for index := range values {
						values[index] = wasm.V128
					}
					return values
				}
				code, entries, err := swarHostThunks([]swarHostSignature{{params: vectorTypes(params), results: vectorTypes(results)}}, MaxCodeBytes)
				if err != nil || len(entries) != 1 || entries[0] != 0 {
					t.Fatal("private vector template", err)
				}
				e, job, args, output, trap, ctrl := thunkFixtureRV64(t, max(64, 2*max(params, results)))
				entry := thunkImageRV64(t, code)
				for cell := 0; cell < 256; cell++ {
					binary.LittleEndian.PutUint64(args[cell*8:], 0x800000007ff00001+uint64(cell)*0x01020304)
				}
				before := bytes.Clone(args)
				for cell := range output {
					output[cell] = 0xa5
				}
				calls := 0
				err = e.CallWithHostBase(entry, args, job.LinMemBase(), trap, output, ctrl, func(context uintptr, index uint32, input, values []uint64) {
					calls++
					if context != thunkPointerRV64(ctrl) || index != 0 || len(input) != 2*params || len(values) != 2*results {
						panic("template argument/result physical cell metadata")
					}
					for cell, value := range input {
						if value != 0x800000007ff00001+uint64(cell)*0x01020304 {
							panic("template vector argument half")
						}
					}
					for cell, value := range values {
						if value != 0 {
							panic("template stale result")
						}
						values[cell] = 0x7ff0000000000001 ^ uint64(cell)*0x0101010101010101
					}
				})
				if err != nil || calls != 1 || !bytes.Equal(args, before) {
					t.Fatal("private vector template call/input preservation", calls, err)
				}
				for cell := 0; cell < 2*results; cell++ {
					if binary.LittleEndian.Uint64(output[cell*8:]) != 0x7ff0000000000001^uint64(cell)*0x0101010101010101 {
						t.Fatal("template vector result half", cell)
					}
				}
				if !bytes.Equal(output[16*results:], bytes.Repeat([]byte{0xa5}, len(output)-16*results)) {
					t.Fatal("template vector result tail overwritten")
				}
			})
		}
	}
}
