//go:build linux && riscv64 && (wago_codegenstats || wago_profile) && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64PublicFinalizedStatistics(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			typ, values := wasm.I64, []uint64{0xabcdef0123456789}
			if vector {
				typ, values = wasm.V128, []uint64{0xabcdef0123456789, 0xfedcba9876543210}
			}
			source := rv64PublicNativeIdentityModule([]wasm.ValType{typ})
			plain, err := Compile(NewRuntimeConfig(), source)
			if err != nil {
				t.Fatal(err)
			}
			defer plain.Close()
			if _, ok := plain.GCNativeCodeTelemetry(); ok {
				t.Fatal("default compile retained telemetry")
			}
			measured, err := Compile(NewRuntimeConfig().WithGCCodeTelemetry(true), source)
			if err != nil {
				t.Fatal(err)
			}
			defer measured.Close()
			if !bytes.Equal(plain.code, measured.code) {
				t.Fatal("statistics changed code")
			}
			got, ok := measured.GCNativeCodeTelemetry()
			if !ok || got.TotalBytes == 0 || got != (GCNativeCodeTelemetry{TotalBytes: uint64(len(measured.code))}) {
				t.Fatal("finalized single-function telemetry", got, ok)
			}
			in, err := Instantiate(measured)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			importedGlobalInvoke(t, in, "identity", values, values...)

			if _, ok := measured.GCNativeCodeTelemetry(); ok {
				t.Fatal("artifact synthesized compilation statistics")
			}
		})
	}
}
