//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceStartCopy(c *Compiled) *Compiled {
	v := rv64ReferenceImportTailCopy(c)
	v.Entry, v.InternalEntry, v.code = slices.Clone(v.Entry), slices.Clone(v.InternalEntry), slices.Clone(v.code)
	return v
}

func TestRV64PublicReferenceFunctionDefinedStartRetainedAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c := rv64NativeTailCompile(t, "reference-start-admission", wasmtest.ClosedReferenceDefinedStart(vector, true, true, 2, false, nil), false)
		if err := rv64ReferenceStartCopy(c).validate(); err != nil {
			t.Fatal(err)
		}
		for name, change := range map[string]func(*Compiled){
			"imported":               func(v *Compiled) { v.StartIsImport = true },
			"inactive-import-index":  func(v *Compiled) { v.StartImportIdx = 1 },
			"inactive-defined-index": func(v *Compiled) { v.HasStart = false; v.StartLocalFunc = 1 },
			"missing":                func(v *Compiled) { v.StartLocalFunc = len(v.Funcs) },
			"negative":               func(v *Compiled) { v.StartLocalFunc = -1 },
			"private":                func(v *Compiled) { v.StartLocalFunc = 2 },
			"numeric-results":        func(v *Compiled) { v.StartLocalFunc = 1 },
			"unaligned":              func(v *Compiled) { v.Entry[0]++ },
			"internal":               func(v *Compiled) { v.InternalEntry[0] += 16 },
			"empty-extent":           func(v *Compiled) { v.Entry[1] = v.Entry[0] },
			"oversized-extent":       func(v *Compiled) { v.Entry[1] = len(v.code) + 16 },
			"frame-packet":           func(v *Compiled) { v.code[v.Entry[0]] ^= 1 },
			"missing-binding":        func(v *Compiled) { v.Funcs[0].HasTypeIndex = false },
			"wrong-binding":          func(v *Compiled) { v.Funcs[0].TypeIndex = 3 },
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				v := rv64ReferenceStartCopy(c)
				change(v)
				if err := v.validate(); err == nil {
					t.Fatal("malformed private start admitted")
				}
				// Inactive union fields are intentionally canonicalized by writers.
				if strings.HasPrefix(name, "inactive-") {
					return
				}
				blob, err := marshalCompiled(v)
				if err != nil {
					return
				}
				loaded, err := LoadTrustedArtifact(blob)
				if loaded != nil {
					loaded.Close()
				}
				if err == nil {
					t.Fatal("malformed private start artifact loaded")
				}
			})
		}
		without := rv64ReferenceStartCopy(c)
		without.HasStart = false
		if err := without.validate(); err != nil {
			t.Fatal("canonical no-start metadata", err)
		}
		// Start selection contributes exactly five bytes to the retained aggregate.
		lo, hi := 0, railshot.MaxModuleBytes
		for lo < hi {
			mid := lo + (hi-lo+1)/2
			v := rv64ReferenceStartCopy(c)
			v.GlobalExports[strings.Repeat("x", mid)] = 2
			if err := rv64ValidateReferenceFunctions(v); err == nil {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		for extra := range 2 {
			v := rv64ReferenceStartCopy(c)
			v.GlobalExports[strings.Repeat("x", lo+extra)] = 2
			if err := rv64ValidateReferenceFunctions(v); (err == nil) != (extra == 0) {
				t.Fatal("retained start exact aggregate", extra, err)
			}
		}
		without.GlobalExports[strings.Repeat("x", lo+5)] = 2
		if err := rv64ValidateReferenceFunctions(without); err != nil {
			t.Fatal("start aggregate charge", err)
		}
		// Destination permission is checked before code publication or owner attachment.
		owners := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
		defer rv64HostCertificateCloseRuntime(t, owners)
		provider, _, _, im, _ := rv64ReferenceStartProvider(t, owners, false, vector, true, true)
		_, nf := rv64ReferenceStartNative(t, owners, false)
		calls := 0
		host := func(v int32) int32 { calls++; return v }
		im.Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", host).Function("env.calls", "owned.fn", host)
		roots := provider.referenceLifetime().snapshot().ResourceRoots
		rt := NewRuntime()
		mod, err := rt.Module(c)
		if err != nil {
			t.Fatal(err)
		}
		in, err := rt.Instantiate(context.Background(), mod, WithImports(im))
		mod.Close()
		rt.Close()
		if in != nil {
			in.Close()
		}
		if in != nil || !errors.Is(err, railshot.ErrUnsupported) || c.loadCodeCache().sealed || c.loadCodeCache().refs != 0 || calls != 0 || provider.referenceLifetime().snapshot().ResourceRoots != roots {
			t.Fatal("default destination published private start", err)
		}
	}
}

func TestRV64PublicReferenceFunctionDefinedStartVoidImportedStart(t *testing.T) {
	c := rv64NativeTailCompile(t, "reference-start-void-import-admission", wasmtest.ClosedReferenceImportedVoidStart(false), false)
	v := cloneCompiledMetadata(c.executionView())
	v.HasStart = true
	v.StartIsImport = true
	if err := rv64ValidateReferenceFunctions(v); err != nil {
		t.Fatal("selected void imported start rejected", err)
	}
}
