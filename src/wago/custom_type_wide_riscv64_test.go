//go:build linux && riscv64 && !tinygo && !wago_precompiled

package wago

import (
	"context"
	"fmt"
	"testing"
)

func TestRV64CustomWideCarrierExecution(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	if err := rt.Use(customCarrierExtension{}); err != nil {
		t.Fatal(err)
	}
	for i, carrier := range []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b, 0x70, 0x6f} {
		t.Run(fmt.Sprintf("carrier-%x", carrier), func(t *testing.T) {
			mod, err := rt.Compile(customCarrierModule(fmt.Sprintf("test.value.%d", i), carrier))
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			in, err := rt.Instantiate(context.Background(), mod)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if _, err := in.Invoke("run"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
