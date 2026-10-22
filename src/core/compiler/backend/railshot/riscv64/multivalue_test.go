//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"
)

type multivalueCall struct{ args, want []uint64 }
type multivalueCase struct {
	name  string
	funcs []fixtureFunc
	calls []multivalueCall
}

func multivalueCases() []multivalueCase {
	const wide = uint64(0x123456789abcdef0)
	pair := []byte{0x7f, 0x7e}
	triple := []byte{0x7f, 0x7e, 0x7f}
	literal := append(sleb([]byte{0x41, 11, 0x42}, int64(wide)), 0x41, 0x7f)
	typeTriple := fixtureFunc{results: triple, body: append(append([]byte(nil), literal...), 0x0b), unexported: true}
	branch := func(op byte) fixtureFunc {
		body := []byte{2, 1, 0x41, 0x3f} // discarded prefix makes the transfer overlap
		body = append(body, literal...)
		body = append(body, 0x20, 0)
		if op == 0x0d {
			body = append(body, op, 0)
		} else {
			body = append(body, op, 2, 0, 0, 0)
		}
		if op == 0x0d {
			body = append(body, 0x1a, 0x1a, 0x1a, 0x1a, 0x41, 22, 0x42, 7, 0x41, 33)
		}
		return fixtureFunc{params: []byte{0x7f}, results: triple, body: append(body, 0x0b, 0x0b)}
	}
	high := fixtureFunc{results: make([]byte, 256)}
	highWant := make([]uint64, 256)
	for j := range high.results {
		if j%2 == 0 {
			high.results[j] = 0x7f
			high.body = sleb(append(high.body, 0x41), int64(-j-1))
			highWant[j] = uint64(uint32(^uint32(j)))
		} else {
			high.results[j] = 0x7e
			high.body = sleb(append(high.body, 0x42), int64(wide+uint64(j)))
			highWant[j] = wide + uint64(j)
		}
	}
	high.body = append(high.body, 0x0b)
	highCaller := fixtureFunc{results: high.results, body: []byte{0x10, 1, 0x0b}}
	wideArgs := bytes.Repeat([]byte{0x7f}, 256)
	bothCaller := fixtureFunc{params: wideArgs, results: high.results}
	for j := range 256 {
		bothCaller.body = uleb(append(bothCaller.body, 0x20), uint32(j))
	}
	bothCaller.body = append(bothCaller.body, 0x10, 1, 0x0b)
	bothCallee := high
	bothCallee.params = wideArgs
	bothArgs := make([]uint64, 256)
	for j := range bothArgs {
		bothArgs[j] = dirty(uint32(j))
	}

	index64 := make([]fixtureFunc, 65)
	for j := range index64 {
		index64[j] = fixtureFunc{body: []byte{0x0b}, unexported: true}
	}
	index64[0] = fixtureFunc{results: pair, body: append(sleb([]byte{2, 0xc0, 0, 0x41, 11, 0x42}, int64(wide)), 0x0b, 0x0b)}
	index64[64] = fixtureFunc{results: pair, body: append(sleb([]byte{0x41, 11, 0x42}, int64(wide)), 0x0b), unexported: true}
	return []multivalueCase{
		{"void", []fixtureFunc{{body: []byte{0x0b}}}, []multivalueCall{{nil, nil}}},
		{"void-call", []fixtureFunc{{body: []byte{0x10, 1, 0x0b}}, {body: []byte{0x0b}}}, []multivalueCall{{nil, nil}}},
		{"identity", []fixtureFunc{{params: pair, results: pair, body: []byte{0x20, 0, 0x20, 1, 0x0b}}}, []multivalueCall{{[]uint64{dirty(0x80000000), wide}, []uint64{0x80000000, wide}}}},
		{"direct-pair", []fixtureFunc{{params: pair, results: pair, body: []byte{0x20, 0, 0x20, 1, 0x10, 1, 0x0b}}, {params: pair, results: pair, body: []byte{0x20, 0, 0x20, 1, 0x0b}}}, []multivalueCall{{[]uint64{dirty(0xffffffff), wide}, []uint64{0xffffffff, wide}}}},
		{"indexed-block-params", []fixtureFunc{{params: pair, results: pair, body: []byte{0x20, 0, 0x20, 1, 2, 0, 1, 0x0b, 0x0b}}}, []multivalueCall{{[]uint64{dirty(17), wide}, []uint64{17, wide}}}},
		{"indexed-if-params", []fixtureFunc{{params: []byte{0x7f, 0x7e, 0x7f}, results: pair, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 4, 1, 0x42, 3, 0x7c, 5, 0x42, 5, 0x7c, 0x0b, 0x0b}}, {params: pair, results: pair, body: []byte{0x20, 0, 0x20, 1, 0x0b}, unexported: true}}, []multivalueCall{{[]uint64{dirty(17), wide, dirty(0)}, []uint64{17, wide + 5}}, {[]uint64{dirty(19), wide, dirty(1)}, []uint64{19, wide + 3}}}},
		{"loop-params-differ-from-results", []fixtureFunc{{params: pair, results: []byte{0x7e, 0x7f}, body: []byte{0x20, 0, 0x20, 1, 3, 0, 0x21, 1, 0x21, 0, 0x20, 0, 0x45, 4, 0x40, 0x20, 1, 0x20, 0, 0x0f, 0x0b, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x42, 3, 0x7c, 0x0c, 0, 0x0b, 0x0b}}}, []multivalueCall{{[]uint64{dirty(0), wide}, []uint64{wide, 0}}, {[]uint64{dirty(7), wide}, []uint64{wide + 21, 0}}}},
		{"overlapping-return", []fixtureFunc{{results: triple, body: append(append([]byte{0x41, 0x3f}, literal...), 0x0f, 0x0b)}}, []multivalueCall{{nil, []uint64{11, wide, 0xffffffff}}}},
		{"overlapping-br-if", []fixtureFunc{branch(0x0d), typeTriple}, []multivalueCall{{[]uint64{dirty(0)}, []uint64{22, 7, 33}}, {[]uint64{dirty(1)}, []uint64{11, wide, 0xffffffff}}}},
		{"overlapping-br-table", []fixtureFunc{branch(0x0e), typeTriple}, []multivalueCall{{[]uint64{dirty(0)}, []uint64{11, wide, 0xffffffff}}, {[]uint64{dirty(1)}, []uint64{11, wide, 0xffffffff}}, {[]uint64{dirty(0xffffffff)}, []uint64{11, wide, 0xffffffff}}}},
		{"256-results", []fixtureFunc{high}, []multivalueCall{{nil, highWant}}},
		{"256-results-call", []fixtureFunc{highCaller, high}, []multivalueCall{{nil, highWant}}},
		{"256-arguments-and-results", []fixtureFunc{bothCaller, bothCallee}, []multivalueCall{{bothArgs, highWant}}},
		{"indexed-if-no-else", []fixtureFunc{{params: []byte{0x7f, 0x7e, 0x7f}, results: pair, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 4, 1, 0x42, 1, 0x7c, 0x0b, 0x0b}}, {params: pair, results: pair, body: []byte{0x20, 0, 0x20, 1, 0x0b}, unexported: true}}, []multivalueCall{{[]uint64{dirty(17), wide, dirty(0)}, []uint64{17, wide}}, {[]uint64{dirty(17), wide, dirty(1)}, []uint64{17, wide + 1}}}},
		{"loop-parameter-result-counts", []fixtureFunc{{params: []byte{0x7f}, results: pair, body: append(sleb([]byte{0x20, 0, 3, 0, 0x21, 0, 0x20, 0, 0x45, 4, 0x40, 0x41, 3, 0x42}, int64(wide)), 0x0f, 0x0b, 0x20, 0, 0x41, 1, 0x6b, 0x0c, 0, 0x0b, 0x0b)}}, []multivalueCall{{[]uint64{dirty(0)}, []uint64{3, wide}}, {[]uint64{dirty(9)}, []uint64{3, wide}}}},
		{"br-table-different-bases", []fixtureFunc{{params: []byte{0x7f}, results: pair, body: append(sleb([]byte{2, 1, 0x41, 0x3f, 2, 1, 0x41, 0x3e, 0x41, 11, 0x42}, int64(wide)), 0x20, 0, 0x0e, 2, 0, 1, 1, 0x0b, 0x1a, 0x1a, 0x1a, 0x41, 22, 0x42, 7, 0x0b, 0x0b)}, {results: pair, body: []byte{0x41, 1, 0x42, 2, 0x0b}, unexported: true}}, []multivalueCall{{[]uint64{dirty(0)}, []uint64{22, 7}}, {[]uint64{dirty(1)}, []uint64{11, wide}}, {[]uint64{dirty(0xffffffff)}, []uint64{11, wide}}}},

		{"signed-block-index64", index64, []multivalueCall{{nil, []uint64{11, wide}}}},
	}
}

func TestMultivalueIntegerExecution(t *testing.T) {
	for _, tc := range multivalueCases() {
		t.Run(tc.name, func(t *testing.T) {
			for _, force := range []bool{false, true} {
				i := callInstance(t, binaryModule(tc.funcs...), nil, force)
				for range 3 {
					for _, call := range tc.calls {
						got := make([]uint64, len(call.want))
						if err := i.InvokeInto(context.Background(), "a", call.args, got); err != nil || !reflect.DeepEqual(got, append([]uint64{}, call.want...)) {
							t.Fatal(force, got, call.want, err)
						}
					}
				}
				if i.compiled.functions[0].Results != len(tc.calls[0].want) {
					t.Fatal("result metadata")
				}
			}
		})
	}
}

func TestMultivalueArityAndTrapDoNotCommit(t *testing.T) {
	set := fixtureFunc{params: []byte{0x7f}, body: []byte{0x20, 0, 0x24, 0, 0x0b}}
	get := fixtureFunc{results: []byte{0x7f, 0x7e}, body: []byte{0x23, 0, 0x42, 7, 0x0b}}
	trap := fixtureFunc{results: []byte{0x7f, 0x7e}, body: []byte{0x10, 3, 0x41, 9, 0x24, 0, 0x0b}}
	leaf := fixtureFunc{results: []byte{0x7f, 0x7e}, body: []byte{0, 0x0b}}
	data := stateModule([]fixtureFunc{set, get, trap, leaf}, false, 0, 1, true, []globalFixture{{mutable: true, value: 3}}, nil)
	i := callInstance(t, data, nil, false)
	// Wrong result arity must be rejected before the side-effecting void entry.
	if _, err := i.Invoke(context.Background(), "a", 17); err == nil {
		t.Fatal("scalar void admitted")
	}
	output := []uint64{0x5555, 0x6666}
	if err := i.InvokeInto(context.Background(), "a", []uint64{17}, output); err == nil {
		t.Fatal("wrong result arity admitted")
	}
	if err := i.InvokeInto(context.Background(), "b", nil, output); err != nil || output[0] != 3 || output[1] != 7 {
		t.Fatal(output, err)
	}
	if err := i.InvokeInto(context.Background(), "a", []uint64{dirty(19)}, nil); err != nil {
		t.Fatal(err)
	}
	output[0], output[1] = 0x5555, 0x6666
	if err := i.InvokeInto(context.Background(), "c", nil, output); err == nil || output[0] != 0x5555 || output[1] != 0x6666 {
		t.Fatal("trap committed", output, err)
	}
	if err := i.InvokeInto(context.Background(), "b", nil, output); err != nil || output[0] != 19 {
		t.Fatal("trap continued side effects", output, err)
	}
	if allocations := testing.AllocsPerRun(50, func() {
		if err := i.InvokeInto(context.Background(), "b", nil, output); err != nil {
			panic(err)
		}
	}); allocations != 0 {
		t.Fatal("warm multivalue allocation", allocations)
	}
}

func TestMultivalueAdmissionBounds(t *testing.T) {
	for _, results := range []int{257, 512} {
		f := fixtureFunc{results: bytes.Repeat([]byte{0x7f}, results), body: []byte{0, 0x0b}}
		if c, err := Compile(binaryModule(f)); c != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal(results, c, err)
		}
	}
	if runtime.GOARCH == "riscv64" {
		t.Log("executed RV64 fixtures; native cache/performance qualification remains outstanding")
	}
}

func TestMultivalueAdmissionAndMetadata(t *testing.T) {
	for _, tc := range multivalueCases() {
		for _, force := range []bool{false, true} {
			c, err := CompileWithOptions(binaryModule(tc.funcs...), CompileOptions{ForceFallback: force})
			if err != nil {
				t.Fatal(tc.name, force, err)
			}
			if c.functions[0].Results != len(tc.calls[0].want) || c.state.resultCapacity < len(tc.calls[0].want) {
				t.Fatal(tc.name, "result metadata")
			}
		}
	}
	// Unreachable transfer work must also count against admission's code budget.
	f := fixtureFunc{results: bytes.Repeat([]byte{0x7f}, 256), body: []byte{0, 2, 0, 0, 0x0e}}
	f.body = uleb(f.body, 64)
	for range 65 {
		f.body = append(f.body, 0)
	}
	f.body = append(f.body, 0x0b, 0x0b)
	if c, err := Compile(binaryModule(f)); c != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("dead branch vector budget", c, err)
	}
}
