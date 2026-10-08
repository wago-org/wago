//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// Runtime destination policy is captured per instance, never written into the
// shared compiled image. Artifact bytes cannot supply allocation authority.
type railshotInstantiateTailPolicy struct{ codeLimit uint64 }

func railshotInstanceTailPolicy(cfg *RuntimeConfig) railshotInstantiateTailPolicy {
	return railshotInstantiateTailPolicy{codeLimit: cfg.maxNativeCodeBytes}
}

type rv64TailAllocationPolicy struct {
	limits                             rv64NativeTailGenerationLimits
	metadataBase                       uint64
	codeConfigured, metadataConfigured bool
	ready                              bool
}

func railshotCaptureNativeTailPolicy(in *Instance, opts InstantiateOptions, arenaNeed int) {
	if !in.c.needsFuncRefDescs() {
		return
	}
	policy := rv64TailAllocationPolicy{limits: rv64NativeTailGenerationLimits{railshot.MaxCodeBytes, railshot.MaxModuleBytes}, metadataBase: uint64(arenaNeed), ready: true}
	codeLimit := opts.railshotTailPolicy.codeLimit
	if !opts.hasExecutionPolicy {
		if indexes := in.c.loadCompileIndexes(); indexes != nil {
			codeLimit = indexes.railshotTail.sourceCodeLimit
		}
	}
	if codeLimit != 0 && codeLimit <= uint64(policy.limits.code) {
		policy.limits.code, policy.codeConfigured = int(codeLimit), true
	}
	if opts.maxInstanceMetadataBytes != 0 {
		// Instantiation already checked final arenaNeed, including policy
		// directories. Do not interpret a zero remainder as unbounded.
		remaining := opts.maxInstanceMetadataBytes - uint64(arenaNeed)
		if remaining <= uint64(policy.limits.metadata) {
			policy.limits.metadata, policy.metadataConfigured = int(remaining), true
		}
	}
	in.ensurePluginState().railshotInvocation.tailPolicy = policy
}

type rv64TailReservationError struct {
	scope, code      bool
	requested, limit int
}

func (e *rv64TailReservationError) Error() string {
	return fmt.Sprintf("%v: RV64 tail generation reservation %d exceeds %d", railshot.ErrUnsupported, e.requested, e.limit)
}
func (*rv64TailReservationError) Unwrap() error { return railshot.ErrUnsupported }

func rv64TailPolicyError(err error, scope, caller rv64TailAllocationPolicy) error {
	var quota *rv64TailReservationError
	if !errors.As(err, &quota) {
		return err
	}
	policy := caller
	if quota.scope {
		policy = scope
	}
	if quota.code && policy.codeConfigured {
		return &coreruntime.ResourceLimitError{Resource: "native code bytes", Scope: "RV64 tail generations", Requested: uint64(quota.requested), Limit: uint64(quota.limit)}
	}
	if !quota.code && policy.metadataConfigured {
		return &coreruntime.ResourceLimitError{Resource: "instance metadata bytes", Scope: "RV64 tail generations", Requested: policy.metadataBase + uint64(quota.requested), Limit: policy.metadataBase + uint64(quota.limit)}
	}
	return err
}
