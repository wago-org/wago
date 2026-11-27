// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone || !wago_standalone_no_plugins

package wago

import (
	"maps"
	"sort"
	"sync/atomic"
)

type runtimePluginCatalog struct {
	plugins        []PluginDefinition
	guestArguments []string
}

func (rt *Runtime) copyGuestArguments() []string {
	return append([]string(nil), rt.guestArguments...)
}

func (rt *Runtime) registerPluginDefinition(definition PluginDefinition) {
	rt.plugins = append(rt.plugins, definition)
}

func (rt *Runtime) clearPluginDefinitions() { rt.plugins = nil }

// Plugins returns immutable definitions in dependency-resolved activation order.
func (rt *Runtime) Plugins() []PluginDefinition {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make([]PluginDefinition, len(rt.plugins))
	for i := range rt.plugins {
		out[i], _ = freezeDefinition(rt.plugins[i])
	}
	return out
}

// WithGuestArguments sets the immutable guest argv visible through the exact
// host.arguments.read authority on this Runtime only.
func WithGuestArguments(args []string) RuntimeOption {
	copyArgs := append([]string(nil), args...)
	return func(rt *Runtime) { rt.guestArguments = copyArgs }
}

type runtimePluginHooks struct {
	hooks          *hookRegistry
	publishedHooks atomic.Pointer[hookRegistry]
}

func (rt *Runtime) publishInitialHooks()            { rt.publishedHooks.Store(rt.hooks) }
func (rt *Runtime) clonePluginHooks() *hookRegistry { return rt.hooks.clone() }

type pluginImportMetadata = map[string]*registeredImport

func lookupPluginImport(metadata pluginImportMetadata, key string) *registeredImport {
	return metadata[key]
}

func setPluginImport(metadata *pluginImportMetadata, key string, imp *registeredImport) {
	(*metadata)[key] = imp
}

func (rt *Runtime) loadHooks() *hookRegistry {
	if rt == nil {
		return &hookRegistry{}
	}
	if hooks := rt.publishedHooks.Load(); hooks != nil {
		return hooks
	}
	return &hookRegistry{}
}

// storeHooks publishes one complete immutable hook generation. The caller must
// hold rt.mu while changing the production generation.
func (rt *Runtime) storeHooks(hooks *hookRegistry) {
	if hooks == nil {
		hooks = &hookRegistry{}
	}
	rt.hooks = hooks
	rt.publishedHooks.Store(hooks)
}

func (rt *Runtime) initializePluginRuntime() {
	rt.hooks = &hookRegistry{}
	rt.imports = resolvedImports{}
	rt.importMeta = map[string]*registeredImport{}
	rt.importOwner = map[string]string{}
	rt.moduleOwner = map[string]string{}
	rt.caps = map[Capability]string{}
	rt.instructions = map[instructionKey]*registeredInstruction{}
	rt.loadingDone = make(chan struct{})
	close(rt.loadingDone)
}

// writableImportsLocked forks both maps once before mutating a published
// generation. Registered metadata is already owned and immutable at insertion.
// The caller holds rt.mu; snapshotModuleBindingsLocked publishes the pair.
func (rt *Runtime) writableImportsLocked() {
	if !rt.importsShared {
		return
	}
	rt.imports = maps.Clone(rt.imports)
	rt.importMeta = maps.Clone(rt.importMeta)
	rt.importsShared = false
}

// ProvidedImports returns immutable metadata for host functions contributed by
// loaded plugins, sorted by Wasm module and name. It intentionally omits each
// callable slotHostFunc binding; this is an inspection surface, not an authority
// escape into the low-level instantiator.
func (rt *Runtime) ProvidedImports() []ImportSpec {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	specs := make([]ImportSpec, 0, len(rt.importMeta))
	for _, meta := range rt.importMeta {
		specs = append(specs, ImportSpec{
			Module:        meta.module,
			Name:          meta.name,
			Kind:          ImportFunc,
			Params:        append([]ValType(nil), meta.params...),
			Results:       append([]ValType(nil), meta.results...),
			Capability:    meta.cap,
			HasCapability: meta.hasCap,
			Docs:          meta.docs,
			Provided:      true,
		})
	}
	sort.Slice(specs, func(i, j int) bool {
		// Compare exact fields because the dotted display key is ambiguous.
		if specs[i].Module != specs[j].Module {
			return specs[i].Module < specs[j].Module
		}
		return specs[i].Name < specs[j].Name
	})
	return specs
}

// Close publishes shutdown and returns promptly. An empty callback-free Runtime
// completes inline; all other teardown continues asynchronously so plugin
// callbacks, host imports, contract calls, invoke hooks, and close observers may
// initiate Runtime closure without self-deadlock. Use CloseContext or WaitClosed
// when completion and the joined teardown error are required.
func (rt *Runtime) resetPluginRegistrationLocked() {
	rt.clearPluginDefinitions()
	rt.imports = resolvedImports{}
	rt.importMeta = map[string]*registeredImport{}
	rt.importOwner = map[string]string{}
	rt.moduleOwner = map[string]string{}
	rt.caps = map[Capability]string{}
	rt.capOrder = nil
	rt.instructions = map[instructionKey]*registeredInstruction{}
	rt.storeHooks(&hookRegistry{})
	resetPluginRuns(&rt.pluginRuns)
	rt.managedActive.Store(false)
	rt.callerResolverActive.Store(false)
}

type pluginInstanceCloseState struct {
	hooks *hookRegistry
	event *InstanceCloseEvent
}

func (in *Instance) prepareInstanceCloseHooks(appendStep func(string, func())) {
	var hooks *hookRegistry
	if in.rt != nil {
		hooks = in.rt.loadHooks()
		for i := len(hooks.internalBeforeClose) - 1; i >= 0; i-- {
			fn := hooks.internalBeforeClose[i]
			appendStep("internal BeforeClose", func() { fn(in) })
		}
	}

	var closeEvent *InstanceCloseEvent
	if hooks != nil && (len(hooks.beforeClose) != 0 || len(hooks.afterClose) != 0) {
		event := InstanceCloseEvent{Module: ModuleView{compiled: in.c, identity: in.moduleIdentity}, Instance: InstanceIdentity{value: in}, Origin: in.instantiateOrigin()}
		closeEvent = &event
		closeState := in.closeState.Load()
		closeState.hooks, closeState.event = hooks, closeEvent
		for i := len(hooks.beforeClose) - 1; i >= 0; i-- {
			fn := hooks.beforeClose[i]
			appendStep("BeforeClose", func() { fn(*closeEvent) })
		}
	}

}

func (state *instanceCloseState) hasPluginAfterClose() bool {
	return state.hooks != nil && len(state.hooks.afterClose) != 0
}

func (state *instanceCloseState) deliverPluginAfterClose(errs []error) []error {
	if state.hooks != nil && state.event != nil {
		for i := len(state.hooks.afterClose) - 1; i >= 0; i-- {
			fn := state.hooks.afterClose[i]
			if err := callShutdownSafely("AfterClose", func() { fn(*state.event) }); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

func (rt *Runtime) snapshotRuntimeCloseHooks() ([]func(RuntimeCloseEvent), []func() error) {
	hooks := rt.loadHooks()
	return append([]func(RuntimeCloseEvent){}, hooks.onRuntimeClose...),
		append([]func() error(nil), hooks.internalClose...)
}

type pluginInstructions = map[instructionKey]*registeredInstruction

func emptyPluginInstructions() pluginInstructions { return nil }

func clonePluginInstructions(instructions pluginInstructions) pluginInstructions {
	snapshot := make(pluginInstructions, len(instructions))
	for key, instruction := range instructions {
		snapshot[key] = instruction
	}
	return snapshot
}

func hasPluginInstructions(instructions pluginInstructions) bool { return len(instructions) != 0 }

func hasPluginInstruction(instructions pluginInstructions, key instructionKey) bool {
	_, exists := instructions[key]
	return exists
}

func addPluginInstruction(instructions *pluginInstructions, instruction *registeredInstruction) {
	(*instructions)[instructionKey{instruction.spec.Module, instruction.spec.Name}] = instruction
}
