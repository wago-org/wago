//go:build !wago_precompiled

package wago

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/wago-org/wago/internal/jitprofile"
)

func (c *Compiled) installCodeProfile(cfg *RuntimeConfig, wasm []byte, stats *railshotModuleStats) error {
	if stats == nil {
		return fmt.Errorf("wago: missing profiling compiler metadata")
	}
	if err := jitprofile.ValidateRegions(stats.ProfileRegions, uint64(len(c.code))); err != nil {
		return fmt.Errorf("wago: finalized profile: %w", err)
	}
	if err := jitprofile.ValidateSources(stats.SourceRanges, uint64(len(c.code))); err != nil {
		return fmt.Errorf("wago: finalized sources: %w", err)
	}
	if err := jitprofile.ValidateCodeSites(stats.CodeSites, uint64(len(c.code))); err != nil {
		return fmt.Errorf("wago: finalized compiler sites: %w", err)
	}
	if err := jitprofile.ValidateCodeSiteRegions(stats.CodeSites, stats.ProfileRegions); err != nil {
		return fmt.Errorf("wago: finalized compiler-site owners: %w", err)
	}
	if err := jitprofile.ValidateUnwind(stats.UnwindRanges, uint64(len(c.code))); err != nil {
		return fmt.Errorf("wago: finalized unwind: %w", err)
	}
	mod := sha256.Sum256(wasm)
	// Effective options are hashed, not environment variables. Hash final bytes as
	// well: process defaults and target-specific choices can change native output.
	config, _ := json.Marshal(struct {
		Features      CoreFeatures
		Optimizations map[string]bool
		Bounds        BoundsCheckMode
		Target        string
	}{cfg.features, cfg.optimizationValues(), c.boundsMode, runtime.GOOS + "/" + runtime.GOARCH})
	h := sha256.New()
	h.Write(mod[:])
	h.Write(config)
	h.Write(c.code)
	im := CodeProfileImage{ModuleID: hex.EncodeToString(mod[:]), ArtifactID: hex.EncodeToString(h.Sum(nil)), Target: runtime.GOOS + "/" + runtime.GOARCH, Regions: stats.ProfileRegions, Unwind: stats.UnwindRanges, Sources: stats.SourceRanges, InlineFrames: stats.SourceFrames}
	if err := jitprofile.ValidateInlineSources(im.Sources, im.InlineFrames); err != nil {
		return err
	}
	if cfg.codeProfile.IncludeSources() {
		im.SourceCoverage = "opcode-lowering-and-deferred-origins"
		im.CodeSites = stats.CodeSites
		im.SiteCoverage = "explicit-operand-spills-reloads-and-memory-bounds-branches"
	}
	if cfg.codeProfile.IncludeUnwind() {
		im.UnwindCoverage = "unsupported"
		if runtime.GOARCH == "amd64" {
			im.UnwindCoverage = "amd64-fixed-frames-and-adapters"
		}
	}
	for i := range im.Regions {
		r := &im.Regions[i]
		if r.Function >= 0 && r.Name == "" {
			r.Name = fmt.Sprintf("wasmfunc%d", r.Function)
		}
	}
	for _, f := range stats.Funcs {
		if f != nil {
			name := f.Name
			if name == "" {
				name = fmt.Sprintf("wasmfunc%d", c.NumImports+f.FuncIdx)
			}
			im.Functions = append(im.Functions, CodeProfileFunction{Index: c.NumImports + f.FuncIdx, Name: name, NativeBytes: f.CodeBytes, FrameBytes: f.FrameBytes, Spills: f.Spills, Reloads: f.Reloads, BoundsChecks: f.BoundsChecks, Calls: f.Calls, Decisions: f.Peephole, Fallback: f.FinalizerFallback})
		}
	}
	c.codeCache.storeProfile(&compiledProfile{session: cfg.codeProfile, image: im, mappings: make(map[uintptr]uint64)})
	return nil
}
