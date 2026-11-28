// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_runtime_no_plugins && (!wago_standalone || !wago_standalone_no_plugins)

package wago

import "sync/atomic"

const pluginRuntimeBuilt = true

type pluginCallbackGate struct {
	gate *pluginCallGate
}

func (b *syncHostBinding) callbackGate() *pluginCallGate         { return b.gate }
func (b *asyncHostBinding) callbackGate() *pluginCallGate        { return b.gate }
func (b *syncHostBinding) setCallbackGate(gate *pluginCallGate)  { b.gate = gate }
func (b *asyncHostBinding) setCallbackGate(gate *pluginCallGate) { b.gate = gate }

type pluginOperationGates []*pluginCallGate

func clonePluginOperationGates(g pluginOperationGates) pluginOperationGates {
	return append(pluginOperationGates(nil), g...)
}

func addPluginOperationGate(g *pluginOperationGates, gate *pluginCallGate) {
	*g = append(*g, gate)
}

// pluginOperationReservation pins the exact plugin generation selected when a
// runtime operation was admitted. Individual callbacks belonging to that
// operation may continue after ordinary callback admission closes, while
// unrelated callbacks still fail closed. A retained reservation keeps every
// participating plugin alive until the operation's terminal callback returns.
type pluginOperationReservation struct {
	gates []*pluginCallGate
	refs  atomic.Int32
}

type pluginReservation = *pluginOperationReservation

func noPluginReservation() pluginReservation                  { return nil }
func hasPluginReservation(reservation pluginReservation) bool { return reservation != nil }
func releasePluginReservation(reservation pluginReservation)  { reservation.release() }
func pluginReservationAllows(reservation pluginReservation, gate *pluginCallGate) bool {
	return reservation.allows(gate)
}

func reservePluginOperation(gates pluginOperationGates) (pluginReservation, error) {
	if len(gates) == 0 {
		return nil, nil
	}
	reservation := &pluginOperationReservation{gates: gates}
	reservation.refs.Store(1)
	for index, gate := range gates {
		if err := gate.enter(); err != nil {
			for prior := index - 1; prior >= 0; prior-- {
				gates[prior].release()
			}
			return nil, err
		}
	}
	return reservation, nil
}

func (r *pluginOperationReservation) release() {
	if r == nil {
		return
	}
	refs := r.refs.Add(-1)
	if refs < 0 {
		panic("wago: plugin operation reservation underflow")
	}
	if refs != 0 {
		return
	}
	for index := len(r.gates) - 1; index >= 0; index-- {
		r.gates[index].release()
	}
}

func (r *pluginOperationReservation) allows(gate *pluginCallGate) bool {
	if r == nil || gate == nil || r.refs.Load() == 0 {
		return false
	}
	for _, candidate := range r.gates {
		if candidate == gate {
			return true
		}
	}
	return false
}

// Only plugin-enabled activations publish reservation owners. The activation
// mutex serializes the inline entry and the overflow map.
type invocationReservations struct {
	reservationID invocationID
	reservation   *pluginOperationReservation
	reservations  map[invocationID]*pluginOperationReservation
}

func (in *Instance) swapInvocationReservation(next *pluginOperationReservation) *pluginOperationReservation {
	if in == nil {
		return nil
	}
	id := in.currentInvocationID()
	if id == 0 {
		return nil
	}
	a := &in.ensurePluginState().activations
	a.mu.Lock()
	if (a.reservation != nil && a.reservationID == id) || (a.reservation == nil && a.reservations[id] == nil) {
		previous := a.reservation
		a.reservationID, a.reservation = id, next
		a.mu.Unlock()
		return previous
	}
	previous := a.reservations[id]
	if next == nil {
		delete(a.reservations, id)
		if len(a.reservations) == 0 {
			a.reservations = nil
		}
	} else {
		if a.reservations == nil {
			a.reservations = make(map[invocationID]*pluginOperationReservation)
		}
		a.reservations[id] = next
	}
	a.mu.Unlock()
	return previous
}

func currentInvocationReservation(in *Instance) *pluginOperationReservation {
	if in == nil {
		return nil
	}
	id := in.currentInvocationID()
	if id == 0 {
		return nil
	}
	a := &in.ensurePluginState().activations
	a.mu.Lock()
	var reservation *pluginOperationReservation
	// The inline capability belongs only to this exact invocation. A different
	// active chain must still resolve its own fallback entry.
	if a.reservationID == id && a.reservation != nil {
		reservation = a.reservation
	} else {
		reservation = a.reservations[id]
	}
	a.mu.Unlock()
	return reservation
}
