//go:build (linux || darwin || windows) && arm64 && !tinygo

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/compilerpair"
	"testing"
)

// These tests are serial: the existing admission switch is process-global.
func compilePathPair(raw []byte, requestShared bool) (compilerpair.Artifact, error) {
	return compilePathPairWithSources(raw, requestShared, false)
}

func compilePathPairWithSources(raw []byte, requestShared, sources bool) (compilerpair.Artifact, error) {
	previous := sharedScalarEnabled
	sharedScalarEnabled = requestShared
	defer func() { sharedScalarEnabled = previous }()
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		return compilerpair.Artifact{}, err
	}
	if err := wasm.ValidateModule(m); err != nil {
		return compilerpair.Artifact{}, err
	}
	var stats ModuleStats
	opts := CompileOptions{Workers: 1, DeferCodeMapping: true, SourceMaps: sources, Profile: sources, Optimizations: map[string]bool{"inline": false}}
	if diagnosticsEnabled {
		opts.Stats = &stats
	}
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		return compilerpair.Artifact{}, err
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	a := compilerpair.Artifact{HostEvents: len(m.Imports) != 0, Code: cm.Code, Entry: cm.Entry}
	if diagnosticsEnabled {
		for _, f := range stats.Funcs {
			a.Shared = append(a.Shared, f.SharedScalar)
			a.Frames = append(a.Frames, f.FrameBytes)
			a.SourceRanges += len(f.SourceRanges)
		}
	}
	return a, nil
}
func TestSharedEstablishedPathPairs(t *testing.T) {
	if profileEnabled {
		t.Skip("profile build uses established path; covered by TestProfileSharedSourceRecordingFallback")
	}
	compilerpair.RunMatrix(t, compilePathPair, diagnosticsEnabled)
}
func BenchmarkSharedEstablishedCompile(b *testing.B) {
	if profileEnabled {
		b.Skip("shared compilation is unavailable in profile builds")
	}
	compilerpair.BenchmarkCompile(b, compilePathPair)
}
func BenchmarkSharedEstablishedExecute(b *testing.B) {
	if profileEnabled {
		b.Skip("shared compilation is unavailable in profile builds")
	}
	compilerpair.BenchmarkExecute(b, compilePathPair)
}

func TestProfileSharedSourceRecordingFallback(t *testing.T) {
	if !profileEnabled {
		t.Skip("requires wago_profile source recording")
	}
	compilerpair.RunSourceFallback(t, func(raw []byte, requested bool) (compilerpair.Artifact, error) {
		return compilePathPairWithSources(raw, requested, true)
	})
}
