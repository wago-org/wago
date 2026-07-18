//go:build wago_guardpage && ((linux && (amd64 || arm64 || riscv64)) || (darwin && arm64))

package wagobench

import (
	"testing"

	wago "github.com/wago-org/wago"
)

// TestGuardPageGlobalGet exercises a non-memory function through the same
// public invocation path used by the benchmark. Guard mode must not require a
// fault to return safely to Go.
func TestGuardPageGlobalGet(t *testing.T) {
	c, err := wago.Compile(nil, globalBenchWasm)
	if err != nil {
		t.Fatal(err)
	}
	in, err := wago.Instantiate(c, wago.InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("global_get"); err != nil {
		t.Fatal(err)
	}
}
