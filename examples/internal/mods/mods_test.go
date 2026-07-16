package mods

import (
	"testing"

	"github.com/wago-org/wago"
)

func TestExampleModuleFactoriesProduceCompilableWasm(t *testing.T) {
	modules := map[string][]byte{
		"add":             Add(),
		"counter":         Counter(),
		"square-via-host": SquareViaHost(),
		"memory-writer":   MemWriter("hello"),
		"import-caller":   ImportCaller("example", "now", "run", []byte{I64}),
		"log-caller":      LogCaller(2, "hello"),
		"metrics-caller":  MetricsCaller("requests", 3),
	}
	for name, wasm := range modules {
		t.Run(name, func(t *testing.T) {
			mod, err := wago.Compile(nil, wasm)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			defer mod.Close()
		})
	}
}

func TestExampleAddAndCounterExecute(t *testing.T) {
	for _, tc := range []struct {
		name string
		wasm []byte
		call string
		args []uint64
		want int32
	}{
		{"add", Add(), "add", []uint64{wago.I32(5), wago.I32(7)}, 12},
		{"counter", Counter(), "inc", nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mod, err := wago.Compile(nil, tc.wasm)
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			in, err := wago.Instantiate(mod)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Invoke(tc.call, tc.args...)
			if err != nil || len(got) != 1 || wago.AsI32(got[0]) != tc.want {
				t.Fatalf("Invoke = %v, %v", got, err)
			}
		})
	}
}
