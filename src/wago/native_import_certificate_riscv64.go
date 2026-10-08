//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"slices"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

// Construction-only mutation ends after the separate thunk mapping is RX and
// synchronized. Physical instance lifetime owns all borrowed transport slices.
type railshotNativeImportConstruction struct {
	records      []rv64NativeImportRecord
	hosts        railshotHostThunkConstruction
	codeBytes    int
	base         uintptr
	mappingBytes int
	published    bool
	capture      bool
}
type rv64NativeImportRecord struct {
	producer                 *Instance
	local, offset, codeBytes int
	target                   railshot.NativeImportTarget
	certificate              *railshot.NativeImportCertificate
}

func railshotNativeImportMetadataBytes(c *Compiled, imports resolvedImports) int {
	n, owned, shared := 0, 0, false
	for i := range c.Imports {
		if _, native := imports[c.functionImportBindingKey(i)].(*InstanceExport); native {
			n++
		} else if h, ok := imports[c.functionImportBindingKey(i)].(*HostFuncRef); ok && h != nil {
			owned++
		} else {
			shared = true
		}
	}
	if n == 0 && (!c.needsFuncRefDescs() || owned == 0 && !shared) {
		return 0
	}
	size := 0
	if n != 0 || owned != 0 && c.needsFuncRefDescs() {
		size = int(unsafe.Sizeof(railshotNativeImportConstruction{}))
	}
	// A table-free importer still owns return PCs used by a dynamic callee.
	// It needs only publication identity; canonical proxy proofs need records.
	if c.needsFuncRefDescs() {
		if n != 0 {
			size += c.NumImports*int(unsafe.Sizeof(rv64NativeImportRecord{})) + n*railshot.NativeImportCertificateBytes()
		}
		if owned != 0 {
			size += c.NumImports*int(unsafe.Sizeof(rv64HostThunkRecord{})) + owned*railshot.NativeTailHostCertificateBytes()
		}
		if shared {
			// Charge the shared construction conservatively to every importer;
			// one instance's retirement cannot refund another's retained cache.
			size += int(unsafe.Sizeof(railshotHostThunkConstruction{})) + c.NumImports*(int(unsafe.Sizeof(rv64HostThunkRecord{}))+railshot.NativeTailHostCertificateBytes())
		}
	}
	return size
}
func railshotInitNativeImportConstruction(s *railshotNativeImportConstruction, c *Compiled, imports resolvedImports) {
	native, owned := false, false
	for i := range c.Imports {
		switch imports[c.functionImportBindingKey(i)].(type) {
		case *InstanceExport:
			native = true
		case *HostFuncRef:
			owned = true
		}
	}
	s.capture = native || owned && c.needsFuncRefDescs()
	if native && c.needsFuncRefDescs() {
		s.records = make([]rv64NativeImportRecord, c.NumImports)
	}
	if owned {
		railshotInitHostThunkConstruction(&s.hosts, c)
	}
}
func railshotRecordNativeImportSpan(s *railshotNativeImportConstruction, index, offset, size int) {
	if len(s.records) != 0 {
		s.records[index].offset, s.records[index].codeBytes = offset, size
	}
	railshotRecordHostThunkSpan(&s.hosts, index, offset)
}
func railshotFinishNativeImportConstruction(s *railshotNativeImportConstruction, mem []byte, base uintptr, size int) {
	if s.capture {
		s.codeBytes, s.base, s.mappingBytes, s.published = size, base, len(mem), true
		railshotFinishHostThunkConstruction(&s.hosts, size)
	}
}
func railshotInstallNativeImportConstruction(in *Instance, s *railshotNativeImportConstruction) {
	if s.capture {
		in.ensurePluginState().railshotInvocation.nativeImports = s
	}
}

// This mapping is separate from the shared guest image. Its owner authenticates
// successful publication and the exact logical extent, never page padding or a
// shared code-cache refcount in place of the particular importer's lease.
type rv64TailThunkCodeOwner struct{ instance *Instance }

func (o rv64TailThunkCodeOwner) NativeTailCodeView() ([]byte, error) {
	in := o.instance
	if in == nil {
		return nil, fmt.Errorf("%w: RV64 import image has no instance", railshot.ErrUnsupported)
	}
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.resourcesClosed || in.c == nil || in.ar == nil || in.jm == nil || in.nativeContext == 0 || in.resourceRefs == 0 && in.invocationState.Load()&instanceInvocationCount == 0 {
		return nil, fmt.Errorf("%w: RV64 import image has no physical lease", railshot.ErrUnsupported)
	}
	state := in.pluginState.Load()
	if state == nil {
		return nil, fmt.Errorf("%w: RV64 import image has no construction owner", railshot.ErrUnsupported)
	}
	s := state.railshotInvocation.nativeImports
	if s == nil || !s.published || s.codeBytes <= 0 || s.codeBytes > len(in.thunkMem) || len(in.thunkMem) != s.mappingBytes || s.base == 0 || offHeapSlicePtr(in.thunkMem) != s.base || s.base&15 != 0 {
		return nil, fmt.Errorf("%w: RV64 import image differs from published construction", railshot.ErrUnsupported)
	}
	return in.thunkMem[:s.codeBytes:s.codeBytes], nil
}

// The caller holds the exact importer/producer graph lease and native
// serialization. Authenticate each physical owner before reading its native
// fields; frozen attachments, ordered numeric types and builder provenance all
// have to agree. The transport payload itself is intentionally mutable.
func (r *rv64NativeImportRecord) validate(in *Instance, index int, image *railshot.NativeTailCodeImage) error {
	if image == nil {
		return fmt.Errorf("%w: RV64 import certificate has no image", railshot.ErrUnsupported)
	}
	view, err := (rv64TailThunkCodeOwner{in}).NativeTailCodeView()
	if err != nil {
		return err
	}
	if err := image.Validate(); err != nil {
		return err
	}
	if image.Base() != offHeapSlicePtr(view) || len(image.Bytes()) != len(view) {
		return fmt.Errorf("%w: RV64 import certificate belongs to a different importer", railshot.ErrUnsupported)
	}
	if index < 0 || index >= in.c.NumImports {
		return fmt.Errorf("%w: RV64 import certificate index", railshot.ErrUnsupported)
	}
	export, ok := rv64NativeFunctionAttachment(in, index)
	if r == nil || !ok || export == nil || export.inst != r.producer || export.localIdx != r.local || r.certificate == nil || r.codeBytes != r.certificate.CodeBytes() {
		return fmt.Errorf("%w: RV64 import certificate attachment", railshot.ErrUnsupported)
	}
	if _, err := (rv64TailCodeOwner{r.producer}).NativeTailCodeView(); err != nil {
		return err
	}
	entry, home, context, valid := rv64QualifiedNativeTailTarget(export)
	if !valid || index < 0 || index >= len(in.c.importFuncSigs) {
		return fmt.Errorf("%w: RV64 import certificate framed target", railshot.ErrUnsupported)
	}
	sig, actual := in.c.importFuncSigs[index], r.producer.c.Funcs[r.local]
	if !slices.Equal(sig.Params, actual.Params) || !slices.Equal(sig.Results, actual.Results) {
		return fmt.Errorf("%w: RV64 import certificate ordered signature", railshot.ErrUnsupported)
	}
	args, err := valTypesSlots(sig.Params)
	if err != nil {
		return err
	}
	results, err := valTypesSlots(sig.Results)
	if err != nil {
		return err
	}
	target := r.target
	target.Entry, target.Home, target.Context = uintptr(entry), uintptr(home), uintptr(context)
	target.ArgumentSlots, target.ResultSlots = args, results
	return r.certificate.Validate(image, r.offset, target, in.jm.LinMemBase(), in.nativeContext, uint32(index))
}
