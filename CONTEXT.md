# Wago terminology

Wago is a managed WebAssembly toolchain. Its command, installed runtimes,
projects, and plugins work as one system.

## Use this glossary

This file gives the preferred names for core Wago concepts. Use a capitalized
term when you mean the exact concept below. **Avoid** lists terms that can cause
confusion in design notes, code, and user documentation.

## Plugins and projects

**Wago Project**:
A directory with a `wago.json` file. The file records selected plugins and
other Wago configuration that belongs to the project.
_Avoid_: Local instance, project config

**Plugin Requirement**:
A Plugin ID and compatible version range selected by a Wago Project or the
shared user scope.
_Avoid_: Plugin dependency, package entry

**Plugin Resolution**:
The exact published Plugin Provider, immutable Plugin Definition, reviewed
Authority Grants, and configuration selected for one Plugin Requirement.
_Avoid_: Plugin state, lock entry

**Plugin ID**:
The canonical Go module or package path that names a plugin wherever it is
published, selected, built, and activated.
_Avoid_: Short name, registry name

**Plugin Definition**:
An immutable description of a plugin. It specifies identity, compatibility,
required plugins, requested Authorities, its configuration contract, and its
provided or required Contracts.
_Avoid_: Extension info, plugin metadata

**Plugin Provider**:
An explicitly linked factory paired with a Plugin Definition.
_Avoid_: Extension factory, self-registration

**Plugin Authority**:
One exact privileged Wago integration power. A plugin can request it and a host
can grant it. Names do not inherit, and a grant gives no unnamed future power.
_Avoid_: Plugin capability, permission group

**Authority Scope**:
The named resources and core-enforced limits within one Plugin Authority. A
grant can equal or narrow a request, but it cannot widen one.
_Avoid_: Capability options, budget

**Plugin Contribution**:
A declarative addition to a runtime plan. Examples include a host import, a
lifecycle observation or interception, a managed resource owner, a compiler
feature, or a Contract binding.
_Avoid_: Hook registration, extension behavior

**Plugin Contract**:
A typed, major-versioned interface. Plugins use it to compose without depending
on another plugin's implementation.
_Avoid_: Service, shared value

**Plugin Plan**:
The complete deterministic graph of resolved Plugin Providers, Authority Grants,
configuration, Contributions, and Contracts. It either commits atomically or
has no runtime effect.
_Avoid_: Load order, plugin list

## Runtime management

**Runtime Installation**:
One installed Wago engine identified by release, profile, and build.
_Avoid_: Version binary, runner

**Active Runtime**:
The Runtime Installation selected to execute runtime commands.
_Avoid_: Current version, active binary

**Runtime Handoff**:
The invocation and metadata that the Wago manager transfers to an Active
Runtime.
_Avoid_: Dispatch environment, runner launch

## Compiler configuration

**Optimization Definition**:
The stable name, description, maturity, default, and supported architectures of
one configurable compiler optimization.
_Avoid_: Knob metadata, optimization flag

**Optimization Binding**:
The architecture-specific code-generation control associated with an
Optimization Definition.
_Avoid_: Backend knob, boolean pointer

**Optimization Selection**:
The immutable set of enabled and disabled Optimization Definitions for one
runtime compilation configuration.
_Avoid_: Global knobs, optimization state

**Runtime Compilation Configuration**:
The immutable Core feature set, function-worker policy, bounds-checking policy,
and Optimization Selection used for one compilation.
_Avoid_: Compiler settings, run flags

## Resource ownership

**Reference Lifetime**:
The period from acquiring a reference owner through logical close, invocation
quiescence, and final release of the owner's physical resources.
_Avoid_: Token lifetime, instance cleanup
