# Wago

Wago provides a managed WebAssembly toolchain whose command, installed runtimes, projects, and plugins behave as one cohesive system.

## Language

**Wago Project**:
A directory whose `wago.json` records plugin intent and other project-owned Wago configuration.
_Avoid_: Local instance, project config

**Plugin Requirement**:
A Plugin ID and compatible version range selected by a Wago Project or the shared user scope.
_Avoid_: Plugin dependency, package entry

**Plugin Resolution**:
The exact published Plugin Provider, immutable Plugin Definition, reviewed Authority Grants, and configuration chosen for one Plugin Requirement.
_Avoid_: Plugin state, lock entry

**Plugin ID**:
The canonical Go module or package path that names one plugin everywhere it is published, selected, built, and activated.
_Avoid_: Short name, registry name

**Plugin Definition**:
An immutable description of a plugin's identity, compatibility, required plugins, requested Authorities, configuration contract, and provided or required Contracts.
_Avoid_: Extension info, plugin metadata

**Plugin Provider**:
An explicitly linked factory paired with one Plugin Definition.
_Avoid_: Extension factory, self-registration

**Plugin Authority**:
One exact privileged Wago integration power a plugin may request and a host may grant; names are non-inheriting and grant no unnamed future power.
_Avoid_: Plugin capability, permission group

**Authority Scope**:
The named resources and core-enforced limits within one Plugin Authority; a grant can equal or narrow a request but cannot widen it.
_Avoid_: Capability options, budget

**Plugin Contribution**:
A declarative addition to a runtime plan, such as a host import, lifecycle observation or interception, managed resource owner, compiler feature, or Contract binding.
_Avoid_: Hook registration, extension behavior

**Plugin Contract**:
A typed, major-versioned interface through which plugins compose without depending on another plugin's implementation.
_Avoid_: Service, shared value

**Plugin Plan**:
The complete deterministic graph of resolved Plugin Providers, Authority Grants, configuration, Contributions, and Contracts that either commits atomically or has no runtime effect.
_Avoid_: Load order, plugin list

**Runtime Installation**:
One installed Wago engine identified by release, profile, and build.
_Avoid_: Version binary, runner

**Active Runtime**:
The Runtime Installation selected to execute runtime commands.
_Avoid_: Current version, active binary

**Runtime Handoff**:
The invocation and metadata transferred from the Wago manager to an Active Runtime.
_Avoid_: Dispatch environment, runner launch

**Optimization Definition**:
The stable name, description, maturity, default, and supported architectures of one configurable compiler optimization.
_Avoid_: Knob metadata, optimization flag

**Optimization Binding**:
The architecture-specific code-generation control associated with an Optimization Definition.
_Avoid_: Backend knob, boolean pointer

**Optimization Selection**:
The immutable set of enabled and disabled Optimization Definitions used for one runtime compilation configuration.
_Avoid_: Global knobs, optimization state

**Runtime Compilation Configuration**:
The immutable Core feature set, function-worker policy, bounds-checking policy, and Optimization Selection used for one compilation.
_Avoid_: Compiler settings, run flags

**Reference Lifetime**:
The period from acquiring a reference owner through logical close, invocation quiescence, and the final release of the owner's physical resources.
_Avoid_: Token lifetime, instance cleanup
