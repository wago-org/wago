//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

const rv64FacSpecSourcesSHA256 = "f0a99e9eee3952e99fca41b9086fe3e16006b3df2740ab1113c2c5d558976733"
const rv64FacSpecFilesSHA256 = "c99d16d27555f6343b48949845c1ff677c77d35a32fd43928e3c53469df158f8"
const rv64CallSpecSourcesSHA256 = "56ae4507085e515eb7b405ade6140989a296f2d9cd582d3cebc0c65ce9f2b5fc"
const rv64CallSpecFilesSHA256 = "21d703c4bea28e7b78f3f022763cbd8ed410695a580aa2b373137d40f220ce4f"

func rv64PublicFacSpecPolicy() rv64PublicSpecPolicy {
	return rv64PublicSpecPolicy{features: CoreFeaturesV2}
}

func rv64PublicCallSpecPolicy() rv64PublicSpecPolicy {
	// call.wast grows its single memory from one page by 0x132 pages.
	// This quota admits that required success and caps live backing at 19.1875 MiB.
	return rv64PublicSpecPolicy{features: CoreFeaturesV2, maxMemories: 1, maxInitialPages: 1, maxTotalInitialPages: 1, maxLivePages: 307}
}

func rv64PublicCallSpecWant(profile string) map[string]int {
	if profile == "fac" {
		return map[string]int{"modules": 1, "assert_return": 6, "assert_exhaustion": 1}
	}
	if profile != "call" {
		panic("unknown call spec profile")
	}
	return map[string]int{"modules": 1, "assert_return": 69, "assert_trap": 1, "assert_exhaustion": 2, "assert_invalid": 18, "assert_invalid_validate": 18}
}

// Both profiles execute every command in source order, through source compilation
// and trusted-artifact roundtrip. Their recursive calls use the shared 512 KiB
// native stack configuration, including its 256 KiB fence margin.
func TestRV64PublicPinnedFacSpecExecution(t *testing.T) {
	rv64PublicCallSpecExecution(t, "fac", "WAGO_RV64_FAC_SPEC_JSON_DIR", rv64PublicFacSpecPolicy())
}

func TestRV64PublicPinnedCallSpecExecution(t *testing.T) {
	rv64PublicCallSpecExecution(t, "call", "WAGO_RV64_CALL_SPEC_JSON_DIR", rv64PublicCallSpecPolicy())
}

func rv64PublicCallSpecExecution(t *testing.T, profile, variable string, policy rv64PublicSpecPolicy) {
	t.Helper()
	dir := os.Getenv(variable)
	if dir == "" {
		t.Skip("prepare pinned fixtures with scripts/prepare-riscv64-numeric-spec.py --profile " + profile + " and set " + variable)
	}
	manifest, err := rv64PublicNumericManifest(dir, profile)
	if err != nil {
		t.Fatal(err)
	}
	rv64PublicSpecRunPolicy(t, dir, manifest, rv64PublicCallSpecWant(profile), policy)
}

// Exhaustion has a separate assertion category. A timeout, interruption,
// untyped error, different trap, or partial result cannot satisfy it.
func rv64PublicSpecExhaustionError(text string, result []uint64, err error) error {
	var trap *coreruntime.TrapError
	if text != "call stack exhausted" || result != nil || !errors.As(err, &trap) || trap == nil || trap.Code != coreruntime.TrapStackFenceBreached {
		return fmt.Errorf("want call stack exhausted and no result for %q, got %v / %v", text, err, result)
	}
	return nil
}

func TestRV64PublicSpecExhaustionGate(t *testing.T) {
	stack := &coreruntime.TrapError{Code: coreruntime.TrapStackFenceBreached}
	for _, tc := range []struct {
		name, text string
		result     []uint64
		err        error
		valid      bool
	}{
		{"stack", "call stack exhausted", nil, stack, true},
		{"wrapped", "call stack exhausted", nil, fmt.Errorf("invoke: %w", stack), true},
		{"success", "call stack exhausted", nil, nil, false},
		{"untyped", "call stack exhausted", nil, errors.New("call stack exhausted"), false},
		{"timeout", "call stack exhausted", nil, context.DeadlineExceeded, false},
		{"interrupt", "call stack exhausted", nil, &coreruntime.TrapError{Code: coreruntime.TrapInterrupted}, false},
		{"wrong-trap", "call stack exhausted", nil, &coreruntime.TrapError{Code: coreruntime.TrapUnreachable}, false},
		{"wrong-diagnostic", "stack overflow", nil, stack, false},
		{"partial-result", "call stack exhausted", []uint64{1}, stack, false},
		{"empty-result", "call stack exhausted", []uint64{}, stack, false},
		{"typed-nil", "call stack exhausted", nil, (*coreruntime.TrapError)(nil), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := rv64PublicSpecExhaustionError(tc.text, tc.result, tc.err); (err == nil) != tc.valid {
				t.Fatalf("accepted=%v want %v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestRV64PublicCallSpecResourcePolicies(t *testing.T) {
	fac, call := rv64PublicFacSpecPolicy(), rv64PublicCallSpecPolicy()
	if fac.config().NativeStackBytes() != MinNativeStackBytes || call.config().NativeStackBytes() != MinNativeStackBytes || call.config().MemoryLimitPages() != 307 {
		t.Fatal("call profile execution quotas differ")
	}
	for _, tc := range []struct {
		name     string
		policy   rv64PublicSpecPolicy
		memories []memoryDef
		valid    bool
	}{
		{"fac-empty", fac, nil, true},
		{"fac-memory", fac, []memoryDef{{}}, false},
		{"call-initial", call, []memoryDef{{Min: 1}}, true},
		{"call-initial-over", call, []memoryDef{{Min: 2}}, false},
		{"call-second", call, []memoryDef{{Min: 1}, {}}, false},
		{"call-wide", call, []memoryDef{{Min: 1, Addr64: true}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Compiled{memoryDir: &compiledMemoryDirectory{defs: tc.memories}}
			if err := tc.policy.checkMemories(c); (err == nil) != tc.valid {
				t.Fatalf("budget accepted=%v want %v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

// Check quota enforcement with two pages, avoiding a second 307-page allocation.
// In particular, a compiled quota does not travel in the trusted artifact.
func TestRV64PublicCallSpecLiveQuotaRoundtrip(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(5, []byte{1, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}))),
	)
	policy := rv64PublicCallSpecPolicy()
	policy.maxLivePages = 2
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			in, err := rv64PublicSpecInstantiatePolicy(t, source, artifact, nil, policy)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := in.close(); err != nil {
					t.Error(err)
				}
			}()
			for _, step := range []struct{ delta, want uint64 }{{1, 1}, {1, 0xffffffff}, {0, 2}} {
				got, err := in.instance.Invoke("grow", step.delta|0xdeadbeef00000000)
				if err != nil || len(got) != 1 || got[0] != step.want {
					t.Fatalf("grow(%d): got %x / %v want %x", step.delta, got, err, step.want)
				}
			}
		})
	}
}
