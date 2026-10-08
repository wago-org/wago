//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func checkSourceIntegerFinal(m *wasm.Module, opts CompileOptions, cm *encoder.CompiledModule) {
	if cm == nil {
		return
	}
	admit := !opts.Profile && !opts.Interruptible && opts.GCFrameRoots == nil && !opts.GCTypeSubtypingRefTest && !opts.GCStructHelpers && !opts.GCArrayHelpers && len(opts.CustomInstructions) == 0 && len(opts.ImportBindings) == 0
	// The known serial CodeBuffer must own exactly the logical bytes checked.
	// Never Take an image here: runtime ownership/sealing happens afterward.
	if cm.CodeImage != nil {
		b, ok := cm.CodeImage.(*coreruntime.CodeBuffer)
		if !ok {
			admit = false
		} else {
			logical := b.Bytes()
			mapping := b.Mapping()
			if len(logical) != len(cm.Code) || len(logical) == 0 || len(mapping) < len(logical) || &logical[0] != &cm.Code[0] || &mapping[0] != &logical[0] {
				admit = false
			}
		}
	}
	shared.VerifySourceIntegerFinalModule(codegen.SourceContextFor(opts.Codegen, m), m, cm.Code, cm.Entry, cm.InternalEntry, admit)
}
