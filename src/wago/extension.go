// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import "context"

// Version is the Wago runtime version used for plugin compatibility checks.
const Version = "0.1.0"

// Stability marks how settled a plugin's public surface is.
type Stability string

const (
	Experimental Stability = "experimental"
	Stable       Stability = "stable"
	Deprecated   Stability = "deprecated"
)

// Compatibility describes the hosts a plugin supports. Only the wago engine
// constraint is enforced by the runtime; other engine and platform entries are
// immutable catalog metadata.
type Compatibility struct {
	Engines   map[string]string `json:"engines,omitempty"`
	Platforms []string          `json:"platforms,omitempty"`
}

// PluginProvenance identifies the source and license behind a linked provider.
type PluginProvenance struct {
	Homepage   string   `json:"homepage,omitempty"`
	Repository string   `json:"repository,omitempty"`
	License    string   `json:"license,omitempty"`
	Authors    []string `json:"authors,omitempty"`
}

// Authority names one exact, non-inheriting privileged Wago interface. Dots
// are presentation grouping only: parents and wildcards are not authorities.
type Authority string

const (
	AuthorityHostImportDefine             Authority = "host.import.define"
	AuthorityHostCallerIdentify           Authority = "host.caller.identify"
	AuthorityHostCallerInvoke             Authority = "host.caller.invoke"
	AuthorityHostArgumentsRead            Authority = "host.arguments.read"
	AuthorityRuntimeCloseObserve          Authority = "runtime.close.observe"
	AuthorityModuleSourceTransform        Authority = "module.source.transform"
	AuthorityModuleCompileObserve         Authority = "module.compile.observe"
	AuthorityModuleCloseObserve           Authority = "module.close.observe"
	AuthorityInstanceInstantiateIntercept Authority = "instance.instantiate.intercept"
	AuthorityInstanceInstantiateObserve   Authority = "instance.instantiate.observe"
	AuthorityInstanceCloseObserve         Authority = "instance.close.observe"
	AuthorityInstanceInvokeIntercept      Authority = "instance.invoke.intercept"
	AuthorityInstanceInvokeObserve        Authority = "instance.invoke.observe"
	AuthorityInstanceManage               Authority = "instance.manage"
	AuthorityCoreModuleCompile            Authority = "core.module.compile"
	AuthorityCoreInstanceInstantiate      Authority = "core.instance.instantiate"
	AuthorityCoreFuncRefCreate            Authority = "core.funcref.create"
	AuthorityCompilerTypeDefine           Authority = "compiler.type.define"
	AuthorityCompilerInstructionDefine    Authority = "compiler.instruction.define"
)

func validAuthority(a Authority) bool {
	switch a {
	case AuthorityHostImportDefine, AuthorityHostCallerIdentify, AuthorityHostCallerInvoke, AuthorityHostArgumentsRead,
		AuthorityRuntimeCloseObserve, AuthorityModuleSourceTransform, AuthorityModuleCompileObserve,
		AuthorityModuleCloseObserve,
		AuthorityInstanceInstantiateIntercept, AuthorityInstanceInstantiateObserve,
		AuthorityInstanceCloseObserve, AuthorityInstanceInvokeIntercept, AuthorityInstanceInvokeObserve,
		AuthorityInstanceManage, AuthorityCoreModuleCompile, AuthorityCoreInstanceInstantiate,
		AuthorityCoreFuncRefCreate, AuthorityCompilerTypeDefine, AuthorityCompilerInstructionDefine:
		return true
	default:
		return false
	}
}

// AuthorityMode says whether installation may omit or narrow a request.
type AuthorityMode string

const (
	AuthorityRequired AuthorityMode = "required"
	AuthorityOptional AuthorityMode = "optional"
)

// AuthorityScope limits one exact authority. Only host.import.define and the
// compiler definition authorities accept Modules. Instance-owning authorities
// require MaxInstances and an aggregate declared-memory MaxMemoryBytes ceiling
// across every live instance owned through the granted handle.
type AuthorityScope struct {
	Modules        []string `json:"modules,omitempty"`
	MaxInstances   uint32   `json:"maxInstances,omitempty"`
	MaxMemoryBytes uint64   `json:"maxMemoryBytes,omitempty"`
}

// AuthorityRequest is immutable, publisher-authored authority metadata.
type AuthorityRequest struct {
	Name   Authority      `json:"name"`
	Mode   AuthorityMode  `json:"mode"`
	Reason string         `json:"reason"`
	Scope  AuthorityScope `json:"scope,omitempty"`
}

// AuthorityGrant is consumer-reviewed authority recorded in the lock graph.
type AuthorityGrant struct {
	Name  Authority      `json:"name"`
	Scope AuthorityScope `json:"scope,omitempty"`
}

// PluginRequirement is an explicit plugin dependency. Version is a semver
// constraint; the selected provider's exact Definition.Version must satisfy it.
type PluginRequirement struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// ContractSpec names one typed composition seam. Major versions are purposely
// incompatible; Go types are additionally checked while registration is planned.
type ContractSpec struct {
	ID    string `json:"id"`
	Major uint32 `json:"major"`
}

// ContractMode selects one required provider, at most one optional provider, or
// every provider in deterministic plugin order.
type ContractMode string

const (
	ContractRequired ContractMode = "required"
	ContractOptional ContractMode = "optional"
	ContractMany     ContractMode = "many"
)

// ContractRequirement declares a typed contract consumed by a plugin.
type ContractRequirement struct {
	ID    string       `json:"id"`
	Major uint32       `json:"major"`
	Mode  ContractMode `json:"mode"`
}

// PluginDefinition is immutable published metadata. It is sufficient to review
// authority and validate the complete dependency graph without executing linked
// plugin code.
type PluginDefinition struct {
	ID            string                `json:"id"`
	Name          string                `json:"name,omitempty"`
	Version       string                `json:"version"`
	Description   string                `json:"description,omitempty"`
	Stability     Stability             `json:"stability,omitempty"`
	Compatibility Compatibility         `json:"compatibility,omitempty"`
	Provenance    PluginProvenance      `json:"provenance,omitempty"`
	Requires      []PluginRequirement   `json:"requires,omitempty"`
	Authorities   []AuthorityRequest    `json:"authorities,omitempty"`
	ConfigSchema  pluginRawMessage      `json:"configSchema,omitempty"`
	Provides      []ContractSpec        `json:"provides,omitempty"`
	Consumes      []ContractRequirement `json:"consumes,omitempty"`
}

// Plugin contributes only declarative registrations. Activation and teardown
// callbacks are declared through Registrar.Lifecycle.
type Plugin interface {
	Register(*Registrar) error
}

// PluginProvider is one explicitly linked definition and factory. Catalogs are
// ordinary values assembled by generated binaries; providers never self-register.
type PluginProvider struct {
	Definition     PluginDefinition
	New            func() Plugin
	ValidateConfig func(pluginRawMessage) error
}

// PluginSelection is one reviewed resolution from the strict lock graph.
type PluginSelection struct {
	ID               string            `json:"id"`
	DefinitionDigest string            `json:"definitionDigest"`
	Direct           bool              `json:"direct"`
	Dependencies     map[string]string `json:"dependencies"`
	Grants           []AuthorityGrant  `json:"grants,omitempty"`
	Contracts        []ContractBinding `json:"contracts,omitempty"`
	Config           pluginRawMessage  `json:"config,omitempty"`
}

type ContractBinding struct {
	ID        string   `json:"id"`
	Major     uint32   `json:"major"`
	Providers []string `json:"providers,omitempty"`
}

// PluginSet pairs an explicit linked catalog with the exact reviewed selections
// to activate. Providers not selected remain inert.
type PluginSet struct {
	Providers  []PluginProvider
	Selections []PluginSelection
}

// PluginLifecycle contains the only activation and teardown callbacks. Start is
// never given a Runtime; privileged work uses revocable handles acquired during
// Register. Stop runs for a failed Start and in reverse dependency order.
type PluginLifecycle struct {
	Start func(context.Context) error
	Stop  func(context.Context) error
}

type PluginPhase string

const (
	PluginPhaseValidate  PluginPhase = "validate"
	PluginPhaseResolve   PluginPhase = "resolve"
	PluginPhaseAuthorize PluginPhase = "authorize"
	PluginPhaseConfigure PluginPhase = "configure"
	PluginPhaseRegister  PluginPhase = "register"
	PluginPhaseCommit    PluginPhase = "commit"
	PluginPhaseStart     PluginPhase = "start"
	PluginPhaseStop      PluginPhase = "stop"
)

// PluginError attributes a failure to one plugin and lifecycle phase.
type PluginError struct {
	Plugin    string
	Phase     PluginPhase
	Authority Authority
	Path      string
	Err       error
}

func (e *PluginError) Error() string {
	return e.diagnosticContext() + ": " + e.Err.Error()
}

func (e *PluginError) diagnosticContext() string {
	where := "wago plugin " + e.Plugin + ": " + string(e.Phase)
	if e.Authority != "" {
		where += " authority " + string(e.Authority)
	}
	if e.Path != "" {
		where += " at " + e.Path
	}
	return where
}

func (e *PluginError) Unwrap() error { return e.Err }

// Capability names a permission a plugin exposes to guest Wasm. Guest
// capabilities are separate from plugin Authorities.
type Capability string

const (
	CapTimerRead       Capability = "timer.read"
	CapNetworkOutbound Capability = "net.outbound"
	CapFilesystemRead  Capability = "fs.read"
	CapFilesystemWrite Capability = "fs.write"
	CapHTTPClient      Capability = "http.client"
	CapKVRead          Capability = "kv.read"
	CapKVWrite         Capability = "kv.write"
	CapMetricsWrite    Capability = "metrics.write"
	CapCompilerCodegen Capability = "compiler.codegen"
)

// sentinelError keeps constant errors comparable and lets the generated facade
// re-export them without copied package variables.
type sentinelError string

func (e sentinelError) Error() string { return string(e) }

const (
	ErrPermissionDenied      = sentinelError("wago: permission denied")
	ErrCallbackPanic         = sentinelError("wago: callback panicked")
	ErrManagedImportLifetime = sentinelError("wago: managed instance cannot inherit a borrowed import")
	ErrMissingImport         = sentinelError("wago: missing import")
	ErrInvalidHandle         = sentinelError("wago: invalid handle")
	ErrPluginConflict        = sentinelError("wago: plugin conflict")
	ErrForeignModule         = sentinelError("wago: module belongs to a different runtime")
)

// PluginPlan is a side-effect-free report built exclusively from immutable
// provider definitions and reviewed selections. No factory or plugin code runs.
type PluginPlan struct {
	Plugins []PluginPlanEntry `json:"plugins"`
}

type PluginPlanEntry struct {
	ID               string                `json:"id"`
	DefinitionDigest string                `json:"definitionDigest"`
	Grants           []AuthorityGrant      `json:"grants,omitempty"`
	Provides         []ContractSpec        `json:"provides,omitempty"`
	Consumes         []ContractRequirement `json:"consumes,omitempty"`
	Contracts        []ContractBinding     `json:"contracts,omitempty"`
}
