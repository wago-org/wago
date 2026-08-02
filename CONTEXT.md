# Wago

Wago provides a managed WebAssembly toolchain whose command, installed runtimes, projects, and plugins behave as one cohesive system.

## Language

**Wago Project**:
A directory whose `wago.json` records plugin intent and other project-owned Wago configuration.
_Avoid_: Local instance, project config

**Plugin Intent**:
The plugin constraints recorded in wago.json and the resolved versions, capabilities, and configuration recorded in wago-lock.json for a Wago Project or the shared user scope.
_Avoid_: Plugin state, dependency list

**Runtime Installation**:
One installed Wago engine identified by release, profile, and build.
_Avoid_: Version binary, runner

**Active Runtime**:
The Runtime Installation selected to execute runtime commands.
_Avoid_: Current version, active binary

**Runtime Handoff**:
The invocation and metadata transferred from the Wago manager to an Active Runtime.
_Avoid_: Dispatch environment, runner launch
