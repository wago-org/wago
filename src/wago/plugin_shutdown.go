// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_runtime_no_plugins && (!wago_standalone || !wago_standalone_no_plugins)

package wago

import "context"

type pluginRunList []registeredPluginRun

type registeredPluginRun struct {
	name           string
	lifecycle      PluginLifecycle
	shouldStop     bool
	consumed       []*contractSlot
	provided       []*contractSlot
	closeInstances []func() error
	drainInstances []func() error
	callbacks      *pluginCallGate
	handles        []func() error
}

func pluginRunsEmpty(pluginRuns pluginRunList) bool { return len(pluginRuns) == 0 }

func clonePluginRuns(pluginRuns pluginRunList) pluginRunList {
	return append(pluginRunList(nil), pluginRuns...)
}

func resetPluginRuns(pluginRuns *pluginRunList) { *pluginRuns = nil }

func markPluginStarted(pluginRuns pluginRunList, index int) { pluginRuns[index].shouldStop = true }

func beginPluginTerminalCallbacks(pluginRuns pluginRunList) {
	for i := len(pluginRuns) - 1; i >= 0; i-- {
		pluginRuns[i].callbacks.beginTerminalCallbacks()
	}
}

func stopAndDrainPlugins(pluginRuns pluginRunList, ctx context.Context, errs []error) []error {
	// Stop every plugin in reverse dependency order before draining callback gates.
	// A provider's Stop can release a host callback already admitted by its consumer.
	for i := len(pluginRuns) - 1; i >= 0; i-- {
		errs = append(errs, stopPluginRun(ctx, pluginRuns[i])...)
	}
	for i := len(pluginRuns) - 1; i >= 0; i-- {
		errs = append(errs, drainPluginRun(pluginRuns[i])...)
	}
	return errs
}

func finishAndRevokePlugins(pluginRuns pluginRunList, errs []error) []error {
	for i := len(pluginRuns) - 1; i >= 0; i-- {
		pluginRuns[i].callbacks.endTerminalCallbacks()
	}
	for i := len(pluginRuns) - 1; i >= 0; i-- {
		errs = append(errs, finishPluginRun(pluginRuns[i])...)
	}
	for i := len(pluginRuns) - 1; i >= 0; i-- {
		for j := len(pluginRuns[i].provided) - 1; j >= 0; j-- {
			var revokeErr error
			panicErr := callShutdownSafely("provided contract revoke", func() { revokeErr = pluginRuns[i].provided[j].revoke() })
			if err := joinPrimary(revokeErr, panicErr); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

func pluginRunsFor(plan []plannedPlugin) pluginRunList {
	runs := make(pluginRunList, len(plan))
	for i := range plan {
		p := &plan[i]
		run := registeredPluginRun{
			name: p.provider.Definition.ID, lifecycle: p.reg.lifecycle,
			closeInstances: append([]func() error(nil), p.reg.closeInstances...),
			drainInstances: append([]func() error(nil), p.reg.drainInstances...),
			callbacks:      p.reg.callGate,
			handles:        append([]func() error(nil), p.reg.revoke...),
		}
		for _, binder := range p.reg.consumes {
			run.consumed = append(run.consumed, binder.contractSlotValue())
		}
		for j := range plan {
			consumer := &plan[j]
			for _, binder := range consumer.reg.consumes {
				binding, _ := contractBindingFor(consumer.selection, binder.contractSpec())
				for _, owner := range binding.Providers {
					if owner == run.name {
						run.provided = append(run.provided, binder.contractSlotValue())
						break
					}
				}
			}
		}
		runs[i] = run
	}
	return runs
}

func stopPluginRun(ctx context.Context, run registeredPluginRun) (errs []error) {
	for i := len(run.closeInstances) - 1; i >= 0; i-- {
		var closeErr error
		panicErr := callShutdownSafely("manager close admission", func() { closeErr = run.closeInstances[i]() })
		if err := joinPrimary(closeErr, panicErr); err != nil {
			errs = append(errs, err)
		}
	}
	run.callbacks.deactivate()
	if run.shouldStop && run.lifecycle.Stop != nil {
		var stopErr error
		panicErr := callShutdownSafely("plugin Stop", func() { stopErr = run.lifecycle.Stop(ctx) })
		if err := joinPrimary(stopErr, panicErr); err != nil {
			errs = append(errs, newPluginDiagnostic(PluginError{Plugin: run.name, Phase: PluginPhaseStop, Err: err}))
		}
	}
	// Close all consumer contract admissions before draining any lease. Unlike
	// host callbacks, a contract lease promises its provider remains live until
	// release, so these drains must finish before the provider's Stop can run.
	// Late terminal observers then receive ErrPermissionDenied instead of
	// entering a provider whose Stop has already completed.
	for i := len(run.consumed) - 1; i >= 0; i-- {
		run.consumed[i].deactivate()
	}
	for i := len(run.consumed) - 1; i >= 0; i-- {
		var revokeErr error
		panicErr := callShutdownSafely("consumed contract revoke", func() { revokeErr = run.consumed[i].revoke() })
		if err := joinPrimary(revokeErr, panicErr); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func drainPluginRun(run registeredPluginRun) (errs []error) {
	for i := len(run.drainInstances) - 1; i >= 0; i-- {
		var drainErr error
		panicErr := callShutdownSafely("manager drain", func() { drainErr = run.drainInstances[i]() })
		if err := joinPrimary(drainErr, panicErr); err != nil {
			errs = append(errs, err)
		}
	}
	if err := run.callbacks.closeAndWait(); err != nil {
		errs = append(errs, err)
	}
	return errs
}

// finishPluginRun retains core-owned handles through terminal observer delivery.
// Contract leases were drained before provider Stop; late observers cannot call
// them, but may still use core handles through this terminal phase.
func finishPluginRun(run registeredPluginRun) (errs []error) {
	for i := len(run.handles) - 1; i >= 0; i-- {
		var closeErr error
		panicErr := callShutdownSafely("plugin handle revoke", func() { closeErr = run.handles[i]() })
		if err := joinPrimary(closeErr, panicErr); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}
