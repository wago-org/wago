//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package semanticcorpus

import (
	"context"
	"errors"
	"testing"
	"time"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestPointerExportsHonorExecutionTimeout(t *testing.T) {
	compiled, err := wago.Compile(nil, nonTerminatingPointerModule())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "single-input",
			run: func() error {
				_, err := runInstance(compiled, Module{
					Invoke: Invoke{Export: "run", Input: "00", InputPtrExport: "pointer"},
					Expect: Expect{Return: []string{"0x0"}},
				}, 20*time.Millisecond)
				return err
			},
		},
		{
			name: "single-output",
			run: func() error {
				_, err := runInstance(compiled, Module{
					Invoke: Invoke{Export: "run", OutputPtrExport: "pointer"},
					Expect: Expect{Memory: []MemoryCell{{Hex: "00"}}},
				}, 20*time.Millisecond)
				return err
			},
		},
		{
			name: "vectors-input",
			run: func() error {
				_, err := runVectorCases(compiled, Module{Invoke: Invoke{Export: "vector_run"}}, &Vectors{
					InputPtrExport: "pointer",
					OutputLen:      1,
					Cases:          []VectorCase{{Out: "00"}},
				}, 20*time.Millisecond)
				return err
			},
		},
		{
			name: "vectors-output",
			run: func() error {
				_, err := runVectorCases(compiled, Module{Invoke: Invoke{Export: "vector_run"}}, &Vectors{
					OutputPtrExport: "pointer",
					OutputLen:       1,
					Cases:           []VectorCase{{Out: "00"}},
				}, 20*time.Millisecond)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() { done <- test.run() }()

			select {
			case err := <-done:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("runner error = %v, want context deadline exceeded", err)
				}
			case <-time.After(time.Second):
				t.Fatal("non-terminating pointer export ignored the execution timeout")
			}
		})
	}
}

func nonTerminatingPointerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x60, 0x00, 0x01, 0x7f},             // () -> i32
			[]byte{0x60, 0x03, 0x7f, 0x7f, 0x7f, 0x00}, // (i32, i32, i32) -> ()
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0),
			wasmtest.ULEB(0),
			wasmtest.ULEB(1),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("pointer", 0, 0),
			wasmtest.ExportEntry("run", 0, 1),
			wasmtest.ExportEntry("vector_run", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x41, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x0b}),
		)),
	)
}
