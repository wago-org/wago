package wagobench

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/tetratelabs/wazero"
)

// TestBinaryTreesWazeroCrossCheck runs each binary-trees variant on wazero (the
// reference engine) and prints its checksum. This disambiguates a wago
// execution bug from an inherent AssemblyScript-runtime property: if a variant
// gives the same "wrong" checksum on both engines, the divergence is AS's own
// GC behavior (e.g. no precise Wasm-stack roots), not wago.
func TestBinaryTreesWazeroCrossCheck(t *testing.T) {
	if os.Getenv("WAGO_BT_REPORT") != "1" {
		t.Skip("set WAGO_BT_REPORT=1 to run the binary-trees wazero cross-check")
	}
	depth := 10
	ctx := context.Background()
	fmt.Printf("\nbinary-trees wazero cross-check  depth=%d\n", depth)
	for _, rt := range btVariants {
		b, err := os.ReadFile(btModulePath(rt))
		if err != nil {
			t.Skipf("binary-trees %s absent: %v", rt, err)
		}
		r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
		if _, err := r.NewHostModuleBuilder("env").
			NewFunctionBuilder().WithFunc(func(_ context.Context, _, _, _, _ int32) {}).
			Export("abort").Instantiate(ctx); err != nil {
			t.Fatalf("wazero env %s: %v", rt, err)
		}
		mod, err := r.Instantiate(ctx, b)
		if err != nil {
			t.Fatalf("wazero instantiate %s: %v", rt, err)
		}
		if init := mod.ExportedFunction("_initialize"); init != nil {
			if _, err := init.Call(ctx); err != nil {
				t.Fatalf("wazero _initialize %s: %v", rt, err)
			}
		}
		res, err := mod.ExportedFunction("run").Call(ctx, uint64(depth))
		if err != nil {
			t.Fatalf("wazero run %s: %v", rt, err)
		}
		fmt.Printf("  %-13s checksum=%d\n", rt, uint32(res[0]))
		r.Close(ctx)
	}
	fmt.Println()
}
