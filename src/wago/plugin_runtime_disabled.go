// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone && wago_standalone_no_plugins

package wago

import "context"

const pluginRuntimeBuilt = false
const errStandalonePluginsDisabled = sentinelError("wago: plugins are disabled in this standalone executable")

type pluginRawMessage = []byte

func DefinitionDigest(PluginDefinition) (string, error) { return "", errStandalonePluginsDisabled }

func CanonicalPluginDefinition(PluginDefinition) (PluginDefinition, error) {
	return PluginDefinition{}, errStandalonePluginsDisabled
}

func EncodeProviderCatalog(string, []PluginProvider) ([]byte, error) {
	return nil, errStandalonePluginsDisabled
}

func DecodeProviderCatalog([]byte) (ProviderCatalogDocument, error) {
	return ProviderCatalogDocument{}, errStandalonePluginsDisabled
}

func InspectPluginPlan(set PluginSet) (*PluginPlan, error) {
	if err := ValidatePluginSet(set); err != nil {
		return nil, err
	}
	return &PluginPlan{Plugins: []PluginPlanEntry{}}, nil
}

func ValidatePluginSet(set PluginSet) error {
	if len(set.Providers) != 0 || len(set.Selections) != 0 {
		return errStandalonePluginsDisabled
	}
	return nil
}

func (rt *Runtime) LoadPlugins(_ context.Context, set PluginSet) error {
	if rt == nil {
		return sentinelError("wago: nil runtime")
	}
	return ValidatePluginSet(set)
}

func (r *Registrar) Config(any) error {
	if err := r.ensureOpen(); err != nil {
		return err
	}
	return errStandalonePluginsDisabled
}

// Bare bindings have no storage for plugin admission.
type pluginCallbackGate = struct{}

func (*syncHostBinding) callbackGate() *pluginCallGate    { return nil }
func (*asyncHostBinding) callbackGate() *pluginCallGate   { return nil }
func (*syncHostBinding) setCallbackGate(*pluginCallGate)  {}
func (*asyncHostBinding) setCallbackGate(*pluginCallGate) {}

type pluginOperationGates = struct{}

func clonePluginOperationGates(pluginOperationGates) pluginOperationGates {
	return pluginOperationGates{}
}
func addPluginOperationGate(*pluginOperationGates, *pluginCallGate) {}

// Bare invocations carry no reservation pointer or storage.
type pluginReservation = struct{}

func noPluginReservation() pluginReservation                          { return pluginReservation{} }
func hasPluginReservation(pluginReservation) bool                     { return false }
func releasePluginReservation(pluginReservation)                      {}
func pluginReservationAllows(pluginReservation, *pluginCallGate) bool { return false }

func reservePluginOperation(pluginOperationGates) (pluginReservation, error) {
	return noPluginReservation(), nil
}

type invocationReservations = struct{}

func (*Instance) swapInvocationReservation(pluginReservation) pluginReservation {
	return noPluginReservation()
}

func currentInvocationReservation(*Instance) pluginReservation { return noPluginReservation() }

type pluginRunList = struct{}

func pluginRunsEmpty(pluginRunList) bool                                           { return true }
func clonePluginRuns(pluginRunList) pluginRunList                                  { return pluginRunList{} }
func resetPluginRuns(*pluginRunList)                                               {}
func markPluginStarted(pluginRunList, int)                                         {}
func beginPluginTerminalCallbacks(pluginRunList)                                   {}
func stopAndDrainPlugins(_ pluginRunList, _ context.Context, errs []error) []error { return errs }
func finishAndRevokePlugins(_ pluginRunList, errs []error) []error                 { return errs }

type runtimePluginCatalog = struct{}

func (*Runtime) copyGuestArguments() []string { return nil }

// WithGuestArguments sets the argv visible to plugins with host.arguments.read.
func WithGuestArguments([]string) RuntimeOption { return func(*Runtime) {} }

func (*Runtime) registerPluginDefinition(PluginDefinition) {}
func (*Runtime) clearPluginDefinitions()                   {}

// Plugins returns the definitions of loaded plugins.
func (*Runtime) Plugins() []PluginDefinition { return []PluginDefinition{} }

type runtimePluginHooks = struct{}

var emptyPluginHooks hookRegistry

func (*Runtime) loadHooks() *hookRegistry        { return &emptyPluginHooks }
func (*Runtime) publishInitialHooks()            {}
func (*Runtime) clonePluginHooks() *hookRegistry { return &emptyPluginHooks }
func (*Runtime) storeHooks(*hookRegistry)        {}

type pluginImportMetadata = struct{}

func lookupPluginImport(pluginImportMetadata, string) *registeredImport { return nil }
func setPluginImport(*pluginImportMetadata, string, *registeredImport)  {}
func (*Runtime) initializePluginRuntime()                               {}
func (*Runtime) writableImportsLocked()                                 {}
func (*Runtime) resetPluginRegistrationLocked()                         {}

// ProvidedImports returns the host imports contributed by loaded plugins.
func (*Runtime) ProvidedImports() []ImportSpec { return []ImportSpec{} }

type pluginInstanceCloseState = struct{}

func (*Instance) prepareInstanceCloseHooks(func(string, func()))         {}
func (*instanceCloseState) hasPluginAfterClose() bool                    { return false }
func (*instanceCloseState) deliverPluginAfterClose(errs []error) []error { return errs }

func (*Runtime) snapshotRuntimeCloseHooks() ([]func(RuntimeCloseEvent), []func() error) {
	return nil, nil
}

type pluginInstructions = struct{}

func emptyPluginInstructions() pluginInstructions                      { return pluginInstructions{} }
func clonePluginInstructions(pluginInstructions) pluginInstructions    { return pluginInstructions{} }
func hasPluginInstructions(pluginInstructions) bool                    { return false }
func hasPluginInstruction(pluginInstructions, instructionKey) bool     { return false }
func addPluginInstruction(*pluginInstructions, *registeredInstruction) {}
