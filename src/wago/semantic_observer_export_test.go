//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo && !wago_precompiled

package wago

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
)

// SemanticCodeEvidenceForTest reads the executable mapping used by this
// instance. A diagnostic recompile must produce those same bytes before its
// compiler-path report is accepted. It supports the import-free profile fixture.
func SemanticCodeEvidenceForTest(t testing.TB, raw []byte, c *Compiled, in *Instance) (hash string, requiredCPU uint32, paths []string) {
	t.Helper()
	cache := c.codeCache
	if cache == nil || cache.base != in.base || len(cache.mem) < len(c.code) {
		t.Fatal("loaded code mapping is unavailable")
	}
	loaded := cache.mem[:len(c.code)]
	if !bytes.Equal(loaded, c.code) {
		t.Fatal("loaded and compiled native bytes differ")
	}
	hash = fmt.Sprintf("%x", sha256.Sum256(loaded))
	requiredCPU = uint32(c.requiredAMD64Features)
	if !compilerTelemetryEnabled {
		return hash, requiredCPU, nil // Unobserved paths must remain inconclusive.
	}
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	if len(c.Imports) != 0 {
		t.Fatal("profile fixture must not import functions")
	}
	slots, err := moduleSyncHostSlotCapacity(m)
	if err != nil {
		t.Fatal(err)
	}
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithFunctionWorkers(1)
	var stats railshotModuleStats
	cm, err := railshotCompileModuleWith(m, railshotCompileOptions{
		Workers: 1, DeferCodeMapping: true, SyncHostSlots: slots,
		Optimizations: cfg.optimizations, OptimizationSnapshot: cfg.optimizationSnapshot, OptimizationDeltas: cfg.optimizationDeltas,
		BitCountFeatures: bitCountHostFeaturesSupported(), Interruptible: !wruntime.HostInterruptSupported(), Stats: &stats,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if !bytes.Equal(cm.Code, loaded) {
		t.Fatal("diagnostic compiler differs from loaded code")
	}
	for _, f := range stats.Funcs {
		path := "established"
		if f.SharedScalar {
			path = "shared"
		}
		paths = append(paths, path)
	}
	return
}

// SemanticCPUProfileForTest reports the selection used by ordinary compilation.
func SemanticCPUProfileForTest() uint32 {
	if runtime.GOARCH != "amd64" {
		return 0
	} // ARM64 baseline NEON has no optional mask here.
	host, _ := cachedAMD64CPUFeatures()
	if !hostSupportsBMI2() {
		host &^= shared.AMD64BMI2
	}
	return uint32(selectedAMD64CompileFeatures(host))
}
