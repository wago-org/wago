//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

type sourceCallVerificationError struct{ Result regalloccheck.Result }

func (e *sourceCallVerificationError) Error() string {
	return "amd64: final source allocation verification: " + e.Result.Message
}

func checkSourceCallsFinal(m *wasm.Module, opts CompileOptions, cm *encoder.CompiledModule) (err error) {
	ctx := codegen.SourceContextFor(opts.Codegen, m)
	defer func() {
		if failure := recover(); failure != nil {
			r := regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: regalloccheck.InvalidGraph, Message: fmt.Sprint(failure)}
			codegen.RecordSourceResult(ctx, 1, r)
			err = &sourceCallVerificationError{Result: r}
		}
	}()
	admit := cm != nil && !opts.Profile && !opts.Interruptible && opts.GCFrameRoots == nil && !opts.GCTypeSubtypingRefTest && !opts.GCStructHelpers && !opts.GCArrayHelpers && len(opts.ImportBindings) == 0 && len(opts.CustomInstructions) == 0
	if cm == nil {
		return nil
	}
	r := shared.VerifySourceCallModuleAMD64(ctx, m, cm.Code, cm.Entry, cm.InternalEntry, admit)
	if r.Verdict == regalloccheck.Verified || r.Verdict == regalloccheck.Rejected || r.Reason == regalloccheck.ResourceLimit {
		codegen.RecordSourceResult(ctx, 1, r)
		if r.Verdict == regalloccheck.Verified {
			codegen.RecordSourceResult(ctx, 2, r)
		}
	}
	if r.Verdict == regalloccheck.Rejected {
		return &sourceCallVerificationError{Result: r}
	}
	return nil
}
