//go:build wago_regalloccheck

package codegen

import (
	"sync"

	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const SourceChecks = true

// Options are shared code-generation dependencies selected by the caller after
// frontend validation and runtime configuration normalization.
type Options struct {
	Runtime RuntimeABI
	Heap    HeapABI
	Module  ModuleInfo
	source  *SourceContext
}

// SourceContext borrows immutable validation facts for one compilation. Workers
// must never mutate it or its module/analysis. Options copies must not reuse it
// for another compilation or share it between concurrent compilations. The owner
// closes it only after all workers finish. It is not heap-policy metadata.
type SourceContext struct {
	module   *wasm.Module
	analysis *wasm.ValidatedModuleAnalysis
	features wasm.ValidationFeatures
	reportMu sync.Mutex
	reporter func(SourceReport)
	reports  []regalloccheck.Result
	// A separate final pass runs after worker quotas have retired. These fixed
	// compile-scoped credits are historical, including failed attempts.
	finalWork, finalStorage int
}

// SourceReport describes one local function's independent source/machine proof.
// Shared-pilot verification is a separate result and cannot satisfy this report.
type SourceReport struct {
	LocalFunction int
	Result        regalloccheck.Result
}

// SetSourceReporter installs a bounded checked-only result sink before workers
// start. Reports are delivered in local-function order after workers join and
// borrowed facts are cleared. The callback must not panic. More than 4096 local
// functions cannot enable this optional collector; compilation still checks
// admitted recipes. A skipped or omitted function is explicitly inconclusive.
func SetSourceReporter(opts Options, m *wasm.Module, reporter func(SourceReport)) bool {
	ctx := SourceContextFor(opts, m)
	if ctx == nil || len(m.Code) > 4096 {
		return false
	}
	ctx.reporter = reporter
	ctx.reports = nil
	if reporter != nil {
		ctx.reports = make([]regalloccheck.Result, len(m.Code))
		for i := range ctx.reports {
			ctx.reports[i] = regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: regalloccheck.UnsupportedOperation, Message: "no admitted source/machine recipe"}
		}
	}
	return true
}

// RecordSourceResult stores only a value result. Retried native attempts replace
// their previous report; no ledger, module, allocator, or scratch escapes.
func RecordSourceResult(ctx *SourceContext, localFunction int, result regalloccheck.Result) {
	if ctx == nil || ctx.reporter == nil {
		return
	}
	ctx.reportMu.Lock()
	defer ctx.reportMu.Unlock()
	if localFunction >= 0 && localFunction < len(ctx.reports) {
		ctx.reports[localFunction] = result
	}
}

// SourceOptions attaches original frontend validation facts to compile options.
func SourceOptions(opts Options, m *wasm.Module, analysis *wasm.ValidatedModuleAnalysis, features wasm.ValidationFeatures) Options {
	AttachSourceContext(&opts, m, analysis, features)
	return opts
}

// AttachSourceContext must receive the exact profile used by frontend validation,
// before execution features are narrowed. ValidFor authenticates module identity,
// not the profile's provenance; callers must keep all borrowed storage immutable.
func AttachSourceContext(opts *Options, m *wasm.Module, analysis *wasm.ValidatedModuleAnalysis, features wasm.ValidationFeatures) bool {
	if opts == nil {
		return false
	}
	opts.source = nil
	if m == nil || !analysis.ValidFor(m) {
		return false
	}
	opts.source = &SourceContext{module: m, analysis: analysis, features: features, finalWork: 16384, finalStorage: 8192}
	return true
}

// ReserveFinalSourcePass charges the one bounded post-layout proof before any
// ledger/model allocation. Zero credits remain exhausted; nothing is refunded.
func ReserveFinalSourcePass(ctx *SourceContext, m *wasm.Module) bool {
	if _, _, ok := ValidatedSourceContext(ctx, m); !ok {
		return false
	}
	ctx.reportMu.Lock()
	defer ctx.reportMu.Unlock()
	if ctx.finalWork < 16384 || ctx.finalStorage < 8192 {
		return false
	}
	ctx.finalWork -= 16384
	ctx.finalStorage -= 8192
	return true
}

// SourceContextFor returns only a matching, still valid borrowed context.
func SourceContextFor(opts Options, m *wasm.Module) *SourceContext {
	if _, _, ok := ValidatedSourceContext(opts.source, m); !ok {
		return nil
	}
	return opts.source
}

// ValidatedSourceContext returns read-only analysis and the original profile.
// Missing or mismatched context is unavailable, never evidence of verification.
func ValidatedSourceContext(ctx *SourceContext, m *wasm.Module) (*wasm.ValidatedModuleAnalysis, wasm.ValidationFeatures, bool) {
	if ctx == nil || m == nil || ctx.module != m || !ctx.analysis.ValidFor(m) {
		return nil, wasm.ValidationFeatures{}, false
	}
	return ctx.analysis, ctx.features, true
}

// CloseSourceContext releases the matching compilation's borrowed facts, even
// if its analysis became stale. All options copies then become unavailable.
// Backends close after compilation; callers also close after early option errors.
// Call only after all users finish. Borrowed module/analysis storage is unchanged.
func CloseSourceContext(opts Options, m *wasm.Module) {
	if opts.source != nil && opts.source.module == m {
		reporter, reports := opts.source.reporter, opts.source.reports
		*opts.source = SourceContext{}
		if reporter != nil {
			for i, result := range reports {
				reporter(SourceReport{LocalFunction: i, Result: result})
			}
		}
	}
}

// Source accounting must not overwrite an independently proved machine verdict
// or a prior quota refusal. Accounting completion still has no machine verdict.
func RecordSourceAccountingResult(ctx *SourceContext, localFunction int, result regalloccheck.Result) {
	if result.Verdict != regalloccheck.Inconclusive {
		return
	}
	if ctx == nil || ctx.reporter == nil {
		return
	}
	ctx.reportMu.Lock()
	defer ctx.reportMu.Unlock()
	if localFunction < 0 || localFunction >= len(ctx.reports) {
		return
	}
	previous := ctx.reports[localFunction]
	if previous.Verdict == regalloccheck.Verified || previous.Verdict == regalloccheck.Rejected || previous.Reason == regalloccheck.ResourceLimit {
		return
	}
	ctx.reports[localFunction] = result
}
