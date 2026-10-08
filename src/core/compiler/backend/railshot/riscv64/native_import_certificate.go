//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"unsafe"
)

// NativeImportCertificate records construction provenance for one ordinary
// native bridge. It never owns a mapping or a physical instance lease. Runtime
// users must retain both endpoint instances and the transport arena separately.
// Fields are immutable after construction; no certificate is serialized.
type NativeImportCertificate struct {
	digest                    [sha256.Size]byte
	target                    nativeImportCertificateTarget
	callerHome, callerContext uintptr
	index                     uint32
	codeBytes                 int
}

type nativeImportCertificateTarget struct {
	entry, home, context, arguments, results, trampoline   uintptr
	argumentBytes, resultBytes, argumentSlots, resultSlots int
	stackOwned, invocation                                 bool
}

func nativeImportCertificateTargetOf(t NativeImportTarget) nativeImportCertificateTarget {
	return nativeImportCertificateTarget{t.Entry, t.Home, t.Context, tailRAPointer(t.Arguments), tailRAPointer(t.Results), t.HostTrampoline, len(t.Arguments), len(t.Results), t.ArgumentSlots, t.ResultSlots, t.StackOwnedActivation, t.InvocationContext}
}

// NativeImportCertificateBytes is the retained Go-metadata charge. Callers
// preflight the aggregate count before constructing any certificates.
func NativeImportCertificateBytes() int { return int(unsafe.Sizeof(NativeImportCertificate{})) }

// NativeImportThunkWithCertificate emits the exact same bridge as
// NativeImportThunk and records its digest and immutable construction inputs.
// Publication still requires the normal W^X/process-wide icache mapper.
func NativeImportThunkWithCertificate(target NativeImportTarget, callerHome, callerContext uintptr, index uint32) ([]byte, *NativeImportCertificate, error) {
	code, err := NativeImportThunk(target, callerHome, callerContext, index)
	if err != nil {
		return nil, nil, err
	}
	return code, &NativeImportCertificate{digest: sha256.Sum256(code), target: nativeImportCertificateTargetOf(target), callerHome: callerHome, callerContext: callerContext, index: index, codeBytes: len(code)}, nil
}

// Validate checks the particular published image owner before touching native
// bytes. It compares the exact bridge extent and retained endpoint/transport
// identities, without regenerating code or inspecting mutable payload contents.
func (c *NativeImportCertificate) Validate(image *NativeTailCodeImage, offset int, target NativeImportTarget, callerHome, callerContext uintptr, index uint32) error {
	if c == nil || image == nil || c.codeBytes <= 0 || c.target != nativeImportCertificateTargetOf(target) || c.callerHome != callerHome || c.callerContext != callerContext || c.index != index {
		return unsupported("native import construction certificate identity")
	}
	if err := image.Validate(); err != nil {
		return err
	}
	code := image.Bytes()
	if offset < 0 || offset&15 != 0 || offset > len(code) || c.codeBytes > len(code)-offset || sha256.Sum256(code[offset:offset+c.codeBytes]) != c.digest {
		return unsupported("native import construction certificate image")
	}
	return nil
}

// CodeBytes reports the exact emitted bridge length, excluding alignment and
// page padding. The runtime records this bound with its imported function row.
func (c *NativeImportCertificate) CodeBytes() int {
	if c == nil {
		return 0
	}
	return c.codeBytes
}
