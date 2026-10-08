//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"slices"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Qualification only. Public feature defaults and SupportedFeatures stay
// closed until all tail instruction/target families have been qualified.
type railshotRuntimeConfigState struct{ tailProducts bool }
type railshotTailCompiledState struct {
	module  *encoder.NativeTailModule
	retired []*railshot.NativeTailDispatcher // codeCache.mu protects failed cleanup
}

func railshotStagedTailFeatures(c *RuntimeConfig) CoreFeatures {
	if c != nil && c.railshotState.tailProducts {
		return CoreFeatureTailCall
	}
	return 0
}
func railshotConfigureNativeTail(required CoreFeatures, opts *railshotCompileOptions) {
	opts.NativeTailCalls = required.IsEnabled(CoreFeatureTailCall)
}
func rv64NativeTailModule(c *Compiled) *encoder.NativeTailModule {
	if x := c.loadCompileIndexes(); x != nil {
		return x.railshotTail.module
	}
	return nil
}
func rv64StoreNativeTailModule(c *Compiled, m *encoder.NativeTailModule) {
	if c.validateMemo == nil {
		c.validateMemo = &validateMemo{}
	}
	c.ensureCompileIndexes().railshotTail.module = m
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= CoreFeatureTailCall
}
func railshotStoreNativeTail(c *Compiled, image *railshotCompiledModule) error {
	if image.NativeTail == nil {
		return nil
	}
	m := &encoder.NativeTailModule{Plans: slices.Clone(image.NativeTail.Plans)}
	rv64StoreNativeTailModule(c, m)
	return nil // finalized signatures are assigned next; validation precedes publication
}
func rv64ValidateNativeTail(c *Compiled) error {
	m := rv64NativeTailModule(c)
	if c.requiredFeatures.IsEnabled(CoreFeatureTailCall) != (m != nil) {
		return fmt.Errorf("RV64 native tail feature/plan contract")
	}
	if m == nil {
		return nil
	}
	if !c.nativeContextTrapABI || len(m.Plans) > 4096 {
		return fmt.Errorf("RV64 native tail context/count contract")
	}
	if _, _, err := railshot.NativeTailInstanceBudget(len(m.Plans), len(c.code)); err != nil {
		return err
	}
	values := 0
	for _, p := range m.Plans {
		local := int(p.FunctionIndex) - c.NumImports
		if local < 0 || local >= len(c.Funcs) || int(p.ImportIndex) >= len(c.importFuncSigs) || p.FrameBytes < 48 || p.FrameBytes > railshot.MaxFrameBytes || p.FrameBytes&15 != 0 || p.SourceBase < 48 || p.SourceBase > p.FrameBytes || p.SourceStride != 8 && p.SourceStride != 16 {
			return fmt.Errorf("RV64 native tail source frame/index contract")
		}
		sig := c.importFuncSigs[p.ImportIndex]
		values += len(sig.Params) + len(sig.Results)
		if err := railshot.NativeTailMetadataBudget(len(m.Plans), values); err != nil {
			return err
		}
		if !slices.Equal(c.Funcs[local].Results, sig.Results) {
			return fmt.Errorf("RV64 native tail result contract")
		}
		at := p.SourceBase
		for _, typ := range sig.Params {
			width := 8
			switch typ {
			case ValI32, ValI64, ValF32, ValF64:
			case ValV128:
				width = 16
				if p.SourceStride != 16 {
					return fmt.Errorf("RV64 native tail pair home")
				}
			default:
				return fmt.Errorf("RV64 native tail parameter type")
			}
			if at > p.FrameBytes-width {
				return fmt.Errorf("RV64 native tail source home extent")
			}
			at += p.SourceStride
		}
	}
	return nil
}

// RV64 architecture 4 + required TailCall determines this appendix. It has no
// addresses, and occupies a fixed position before GC descriptors/root maps.
func railshotWriteNativeTail(w *compiledWriter, c *Compiled) error {
	if err := rv64ValidateNativeTail(c); err != nil {
		return err
	}
	m := rv64NativeTailModule(c)
	if m == nil {
		return nil
	}
	w.u32(1)
	w.uvar(uint64(len(m.Plans)))
	for _, p := range m.Plans {
		w.u32(p.FunctionIndex)
		w.u32(p.ImportIndex)
		w.u32(p.SourcePC)
		w.u32(uint32(p.FrameBytes))
		w.u32(uint32(p.SourceBase))
		w.u32(uint32(p.SourceStride))
	}
	return nil
}
func railshotReadNativeTail(r *compiledReader, c *Compiled) error {
	if !c.requiredFeatures.IsEnabled(CoreFeatureTailCall) {
		return nil
	}
	v, err := r.u32()
	if err != nil {
		return err
	}
	if v != 1 {
		return fmt.Errorf("RV64 native tail plan version %d", v)
	}
	n, err := r.countElements("RV64 native tail plans", 24, unsafe.Sizeof(encoder.NativeTailPlan{}))
	if err != nil {
		return err
	}
	if n > 4096 {
		return fmt.Errorf("RV64 native tail plan count")
	}
	m := &encoder.NativeTailModule{Plans: make([]encoder.NativeTailPlan, n)}
	for i := range m.Plans {
		var fields [6]uint32
		for j := range fields {
			fields[j], err = r.u32()
			if err != nil {
				return err
			}
		}
		m.Plans[i] = encoder.NativeTailPlan{FunctionIndex: fields[0], ImportIndex: fields[1], SourcePC: fields[2], FrameBytes: int(fields[3]), SourceBase: int(fields[4]), SourceStride: int(fields[5])}
	}
	rv64StoreNativeTailModule(c, m)
	return rv64ValidateNativeTail(c)
}
func railshotNativeTailArenaBytes(c *Compiled) int {
	bytes := rv64TailTargetsArenaBytes(c)
	if m := rv64NativeTailModule(c); m != nil && len(m.Plans) != 0 {
		_, arena, err := railshot.NativeTailInstanceBudget(len(m.Plans), len(c.code))
		if err == nil {
			return bytes + arena
		}
	}
	return bytes
}
func railshotNativeCodeBytes(c *Compiled) uint64 {
	size := len(c.code)
	if m := rv64NativeTailModule(c); m != nil {
		code, _, err := railshot.NativeTailInstanceBudget(len(m.Plans), size)
		if err != nil {
			return ^uint64(0)
		}
		size += code
	}
	return uint64(size)
}

func railshotNativeTailImport(c *Compiled, index int) bool {
	m := rv64NativeTailModule(c)
	if m != nil {
		for _, p := range m.Plans {
			if int(p.ImportIndex) == index {
				return true
			}
		}
	}
	return false
}
