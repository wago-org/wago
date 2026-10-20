#!/usr/bin/env bash
set -euo pipefail
# Usage: native-pairs-49dd0664.sh REPO BASE HEAD OUT CAMPAIGN
repo=$(realpath "$1");base=$2;head=$3;out=$(realpath -m "$4");campaign=$(realpath "$5")
mkdir -p "$out"
case "$(uname -m)/$(go env GOARCH)" in x86_64/amd64) ;; *) echo 'Native AMD64 required' >&2;exit 2;;esac
export GOMAXPROCS=4
{ date -u;uname -a;go version;go env GOOS GOARCH;lscpu;printf 'baseline=%s\ncandidate=%s\nGOMAXPROCS=4\n' "$base" "$head"; } > "$out/environment.txt"
base_added=0;head_added=0
cleanup(){ if ((base_added));then git -C "$repo" worktree remove --force "$out/base-tree";fi;if ((head_added));then git -C "$repo" worktree remove --force "$out/head-tree";fi; }
trap cleanup EXIT
git -C "$repo" worktree add --detach "$out/base-tree" "$base";base_added=1
git -C "$repo" worktree add --detach "$out/head-tree" "$head";head_added=1
cat > "$out/width-cost_test.go" <<'WIDTH_GO'
//go:build !tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type widthCostExt struct{ calls *uint64 }

func (widthCostExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.width-cost", Version: "1.0.0", Stability: Experimental}
}
func (e widthCostExt) Register(r *Registry) error {
	r.Capability(CapCompilerCodegen)
	for _, spec := range []struct {
		name   string
		width  int32
		native bool
	}{{"narrow", 4, true}, {"wide", 32, true}, {"portable", 4, false}} {
		spec := spec
		var lower InstructionLowerer
		if spec.native {
			lower = func(c LoweringContext) error { c.Output(0, c.Input(0)); return nil }
		}
		if err := r.Compiler().Instruction(InstructionSpec{Module: "wago:instr/width-cost", Name: spec.name, Input: []int32{spec.width}, Output: []int32{spec.width}, Lower: lower,
			Handler: func(_ InstructionContext, args []Bits) ([]Bits, error) {
				*e.calls++
				out, err := BitsFromUint32(spec.width, args[0].Uint32())
				return []Bits{out}, err
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func widthCostModule(mode string) []byte {
	// Each iteration changes the input, adds a physical high bit, then calls the
	// identity recipe. Baseline narrow results are wrong; timing measures fix cost.
	body := []byte{0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1}
	arg := []byte{0x20, 1, 0x20, 0, 0x6a, 0x41, 0x80, 0x80, 0x80, 0x80, 0x78, 0x72}
	if mode == "mixed" {
		body = append(body, 0x20, 0, 0x41, 1, 0x71, 0x04, 0x7f)
		body = append(body, arg...)
		body = append(body, 0x10, 0, 0x05)
		body = append(body, arg...)
		body = append(body, 0x10, 1, 0x0b)
	} else {
		idx := byte(0)
		if mode == "wide" {
			idx = 1
		}
		if mode == "fallback" {
			idx = 2
		}
		body = append(body, arg...)
		body = append(body, 0x10, idx)
	}
	body = append(body, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/width-cost", "narrow", 0), instructionFuncImport("wago:instr/width-cost", "wide", 0), instructionFuncImport("wago:instr/width-cost", "portable", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 3))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func widthCostRuntime(t testing.TB, calls *uint64) *Runtime {
	t.Helper()
	rt := NewRuntime()
	t.Cleanup(func() { rt.Close() })
	if err := rt.Use(widthCostExt{calls: calls}); err != nil {
		t.Fatal(err)
	}
	return rt
}

func TestCampaignWidthCostProbe(t *testing.T) {
	for _, mode := range []string{"fast", "wide", "fallback", "mixed"} {
		var calls uint64
		rt := widthCostRuntime(t, &calls)
		mod, err := rt.Compile(widthCostModule(mode))
		if err != nil {
			t.Fatal(err)
		}
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			t.Fatal(err)
		}
		out, err := in.Invoke("run", I32(256), I32(123))
		if err != nil || len(out) != 1 {
			t.Fatalf("%s: %v %v", mode, out, err)
		}
		want := uint32(123)
		for n := uint32(256); n > 0; n-- {
			want = (want + n) | 0x80000000
			if mode == "fast" || mode == "fallback" || (mode == "mixed" && n&1 != 0) {
				want &= 15
			}
		}
		expectedCalls := uint64(0)
		if mode == "fallback" {
			expectedCalls = 256
		}
		if calls != expectedCalls {
			t.Fatalf("%s helper calls=%d want%d", mode, calls, expectedCalls)
		}
		t.Logf("%s output=%#x correct=%t helpers=%d operations=256 code-B=%d code-sha256=%x", mode, uint32(out[0]), uint32(out[0]) == want, calls, mod.Compiled().CodeSize(), sha256.Sum256(mod.Compiled().code))
		if os.Getenv("WAGO_WIDTH_REQUIRE_CORRECT") == "1" && uint32(out[0]) != want {
			t.Fatalf("%s want %#x", mode, want)
		}
		if root := os.Getenv("WAGO_WIDTH_CODE_DIR"); root != "" {
			if err := os.MkdirAll(root, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, mode+".bin"), mod.Compiled().code, 0644); err != nil {
				t.Fatal(err)
			}
		}
		in.Close()
		mod.Close()
	}
}

func BenchmarkWidthCostInvoke(b *testing.B) {
	for _, mode := range []string{"fast", "wide", "fallback", "mixed"} {
		b.Run(mode, func(b *testing.B) {
			var calls uint64
			rt := widthCostRuntime(b, &calls)
			mod, err := rt.Compile(widthCostModule(mode))
			if err != nil {
				b.Fatal(err)
			}
			defer mod.Close()
			in, err := rt.Instantiate(context.Background(), mod)
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := in.Invoke("run", I32(256), I32(123)); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(calls)/float64(b.N), "helpers/op")
			b.ReportMetric(256, "guest-ops/op")
			b.ReportMetric(float64(mod.Compiled().CodeSize()), "code-B")
		})
	}
}

func BenchmarkWidthCostCompile(b *testing.B) {
	for _, mode := range []string{"fast", "wide", "fallback", "mixed"} {
		b.Run(mode, func(b *testing.B) {
			var calls uint64
			rt := widthCostRuntime(b, &calls)
			module := widthCostModule(mode)
			var size int
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				mod, err := rt.Compile(module)
				if err != nil {
					b.Fatal(err)
				}
				size = mod.Compiled().CodeSize()
				mod.Close()
			}
			b.StopTimer()
			b.ReportMetric(float64(size), "code-B")
		})
	}
}
WIDTH_GO
for stage in base head;do
 cp "$out/width-cost_test.go" "$out/$stage-tree/src/wago/campaign_width_cost_test.go"
 git -C "$repo" show "$head:src/wago/instruction_recipe_width_test.go" > "$out/$stage-tree/src/wago/instruction_recipe_width_test.go"
 for file in campaign_width_cost_test.go instruction_recipe_width_test.go;do
  sha256sum "$out/$stage-tree/src/wago/$file" >> "$out/overlay-sha256.txt"
  git -C "$out/$stage-tree" add -N -- "src/wago/$file"
 done
 git -C "$out/$stage-tree" diff --binary > "$out/$stage-test-overlay.diff"
 (cd "$out/$stage-tree";python3 "$campaign/run.py" build go test -p 4 -c -o "$out/$stage.test" ./src/wago)
done
(cd "$out/head-tree";python3 "$campaign/run.py" build go test -p 4 -c -tags=wago_regalloccheck -o "$out/head-debug.test" ./src/wago)
sha256sum "$out/base.test" "$out/head.test" "$out/head-debug.test" > "$out/binary-sha256.txt"
# The baseline should fail the public regression; record it as an oracle.
(cd "$out/base-tree/src/wago";python3 "$campaign/run.py" build "$out/base.test" -test.run '^TestCustomInstructionInputRecipeTrapOrder$' -test.count=1 -test.v) > "$out/base-trap-order-control.txt" 2>&1
set +e
(cd "$out/base-tree/src/wago";python3 "$campaign/run.py" build "$out/base.test" -test.run '^TestCustomInstructionInputRecipeWidth$' -test.count=1 -test.v) > "$out/base-expected-regression.txt" 2>&1
base_status=$?
set -e
printf '%s\n' "$base_status" > "$out/base-expected-regression.status"
if ((base_status!=1));then echo 'Unexpected baseline regression status' >&2;exit 3;fi
(cd "$out/base-tree/src/wago";python3 "$campaign/run.py" build env WAGO_WIDTH_CODE_DIR="$out/base-code" "$out/base.test" -test.run '^TestCampaignWidthCostProbe$' -test.count=1 -test.v) > "$out/base-probe.txt" 2>&1
for binary in head head-debug;do
 (cd "$out/head-tree/src/wago";python3 "$campaign/run.py" build env WAGO_WIDTH_CODE_DIR="$out/$binary-code" WAGO_WIDTH_REQUIRE_CORRECT=1 "$out/$binary.test" -test.run '^TestCampaignWidthCostProbe$|^TestCustomInstruction' -test.count=1 -test.v) > "$out/$binary-correctness.txt" 2>&1
done
python3 "$campaign/run.py" bench env GOMAXPROCS=4 bash -s -- "$out" <<'BENCH'
set -euo pipefail
out=$1
for pair in 1 2 3 4 5 6 7;do
 order='base head';if ((pair%2==0));then order='head base';fi
 for stage in $order;do
  (cd "$out/$stage-tree/src/wago"
   "$out/$stage.test" -test.run '^$' -test.bench '^(BenchmarkWidthCost(Invoke|Compile)|BenchmarkInvokeHostFuncDirect)$' -test.benchmem -test.benchtime=250ms -test.count=1 -test.cpu=4 > "$out/pair-$pair-$stage.txt" 2>&1
   "$out/$stage.test" -test.run '^$' -test.bench '^BenchmarkHostRoundtripLoop$/^mem1$/^parallel(false|true)$/^host[01]$/^n(0|64|1024)$' -test.benchmem -test.benchtime=250ms -test.count=1 -test.cpu=4 >> "$out/pair-$pair-$stage.txt" 2>&1
  )
 done
done
BENCH
