//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	rv64runtime "github.com/wago-org/wago/src/core/runtime/riscv64"
)

// AdmitProductionSource checks wire provenance before the decoded public
// frontend discards present empty sections and explicit singleton rec groups.
// It is an execution policy for already decoded/validated source, not a decoder.
func AdmitProductionSource(data []byte) error {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0, 'a', 's', 'm', 1, 0, 0, 0}) {
		return fmt.Errorf("rv64: invalid source header")
	}
	if len(data) > MaxModuleBytes {
		return unsupported("bounded source size")
	}
	if err := admitWireStartProfile(data, true, true, true); err != nil {
		return err
	}
	// Public state/import/table support makes their empty sections inert.
	// Private pilots preserve the original nonempty-section execution policy.
	return admitProductionWireProfile(data, true)
}

// CompileModuleWith compiles decoded modules for the production wrapper ABI.
// The public integration also calls AdmitProductionSource. Current bounded
// admission is deliberately retained until the complete port is qualified.
func CompileModuleWith(m *wasm.Module, opts CompileOptions) (*enc.CompiledModule, error) {
	if m == nil {
		return nil, fmt.Errorf("rv64: nil module")
	}
	if err := AdmitPublicInitializers(m); err != nil {
		return nil, err
	}
	if _, err := admitTableProfile(m, true, opts.Interruptible && opts.ForeignTableCalls); err != nil {
		return nil, err
	}
	if len(m.Imports) > MaxProductionImports || m.MemCount() > MaxProductionMemories || len(m.Code) > MaxFunctions || len(m.Types) > MaxFunctions || len(m.Globals) > 256 || len(m.Data) > rv64runtime.MaxDataSegments || len(opts.Codegen.Module.GCTypeDescs) > MaxFunctions {
		return nil, unsupported("decoded module count bound")
	}
	// Decoded callers do not necessarily carry the original byte-size limit.
	// Bound instruction and data bytes before validation or payload copies.
	remaining := MaxModuleBytes
	consume := func(size int) bool {
		if size > remaining {
			return false
		}
		remaining -= size
		return true
	}
	for _, fn := range m.Code {
		if !consume(len(fn.BodyBytes)) {
			return nil, unsupported("decoded instruction/data size bound")
		}
	}
	for _, data := range m.Data {
		if !consume(len(data.Init)) {
			return nil, unsupported("decoded instruction/data size bound")
		}
	}
	usesSWAR, err := moduleUsesSWAR(m)
	if err != nil {
		return nil, err
	}
	if usesSWAR {
		return compileSWARProductionModule(m, opts)
	}
	if len(m.Customs) > MaxFunctions {
		return nil, unsupported("decoded custom section count bound")
	}
	// Reuse the existing decoded metadata accounting with the instruction/data
	// budget. Opaque custom payloads must stay bounded for hand-built callers,
	// and the decoded/raw name views must not duplicate retained byte charges.
	if err := admitSWARDecodedMetadataBudget(m, &remaining); err != nil {
		return nil, err
	}
	if err := admitProductionOptions(m, &opts, false); err != nil {
		return nil, err
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{ExtendedConstGlobals: true, MultiMemory: true}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
		return nil, err
	}
	opts.production = true
	opts.publicStarts = true
	c, err := compileDecodedModule(m, scalarRegisters[:], opts)
	if err != nil {
		return nil, err
	}
	return stageProductionModule(c, opts)
}

// admitProductionOptions keeps semantic option admission identical across the
// scalar compiler and the private SWAR production staging prerequisite.
func admitProductionOptions(m *wasm.Module, opts *CompileOptions, allowSWAR bool) error {
	if opts.ForeignTableCalls && !opts.Interruptible {
		return unsupported("foreign table calls require the cooperative framed runtime ABI")
	}
	if err := admitProductionStart(m); err != nil {
		return err
	}
	if len(opts.Codegen.Module.GCTypeDescs) > MaxFunctions {
		return unsupported("production function descriptor count bound")
	}
	if opts.SyncHostSlots < 0 || opts.SyncHostSlots > 65535 {
		return fmt.Errorf("rv64: invalid synchronous host slot capacity %d", opts.SyncHostSlots)
	}
	if err := admitHostBindings(m, opts); err != nil {
		return err
	}
	if opts.ElideBoundsChecks || opts.GCTypeSubtypingRefTest || opts.GCStructHelpers || opts.GCArrayHelpers || opts.GCFrameRoots != nil || len(opts.CustomInstructions) != 0 || len(opts.Codegen.Module.GCTypeLayouts) != 0 || opts.Codegen.Runtime != nil || opts.Codegen.Heap != nil || opts.Codegen.Module.Features.GC || !allowSWAR && opts.Codegen.Module.Features.V128 {
		return unsupported("production host/guard/GC/custom lowering is pending")
	}
	// The public frontend emits inert function sentinels for numeric types.
	// They do not require heap lowering, but nonfunction descriptors do.
	for _, desc := range opts.Codegen.Module.GCTypeDescs {
		if desc.Kind != gc.KindFunc || !desc.Final || desc.HasSuper {
			return unsupported("production GC descriptor lowering is pending")
		}
	}
	if err := gc.ValidateTypeDescs(opts.Codegen.Module.GCTypeDescs); err != nil {
		return err
	}
	if opts.Stats != nil && !diagnosticsEnabled {
		return unsupported("compiler diagnostics require -tags=wago_codegenstats or wago_profile")
	}
	if opts.UnwindMaps || opts.CollectInlineReport {
		return unsupported("unwind metadata and inline reports are pending")
	}
	if opts.Profile && !profileEnabled {
		return unsupported("profiling requires -tags=wago_profile")
	}
	if opts.SourceMaps && !opts.Profile {
		return unsupported("source maps require profiling")
	}
	if opts.Profile && opts.Stats == nil {
		opts.Stats = &ModuleStats{}
	}
	if _, err := optimizationBindings.ResolveSnapshot(opts.Optimizations, opts.OptimizationSnapshot, opts.OptimizationDeltas); err != nil {
		return err
	}
	return nil
}

func stageProductionModule(c *Compiled, opts CompileOptions) (*enc.CompiledModule, error) {
	if err := c.prepareProfile(len(c.code)); err != nil {
		return nil, err
	}
	if opts.MemoryPressure != nil && (opts.MemoryPressureAt == 0 || len(c.code) >= opts.MemoryPressureAt) {
		opts.MemoryPressure()
	}
	image, err := stageProduction(c, opts.DeferCodeMapping)
	if err != nil {
		return image, err
	}
	// The shared compiler contract represents deferred heap ownership as Code
	// with no CodeImage. A nonnil CodeImage promises Take can transfer an RW
	// mapping to the public cache; heap staging cannot fulfill that promise.
	if opts.DeferCodeMapping && image.CodeImage != nil {
		image.Code, err = image.CodeImage.(*coreruntime.CodeBuffer).TakeHeap()
		if err != nil {
			return nil, err
		}
		image.CodeImage = nil
	}
	if diagnosticsEnabled && opts.Stats != nil {
		collectFinalizedStats(c, opts.Stats)
	}
	return image, nil
}
