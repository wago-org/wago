// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (!linux && !(darwin && arm64)) || (!amd64 && !arm64) || tinygo || wago_target_tinygo

package runtime

import "time"

type interruptActivation struct{}

func beginInterruptActivation([]byte) (*interruptActivation, error) { return nil, nil }
func (*interruptActivation) enterWasm(uintptr)                      {}
func (*interruptActivation) leaveWasm()                             {}
func (*interruptActivation) close()                                 {}

func registerExecutableCode([]byte) error { return nil }
func unregisterExecutableCode([]byte)     {}

// RequestInterrupt publishes an interruption for cooperative native safepoints.
func RequestInterrupt(trap []byte) {
	storeTrap(trap, uint32(TrapInterrupted))
}

func RequestInterruptAsync(trap []byte) func() {
	RequestInterrupt(trap)
	return func() {}
}
func SetInterruptDeadline([]byte, time.Time) (func(), error) { return func() {}, nil }

// HostInterruptSupported reports whether this build can asynchronously unwind
// generated Wasm without compiler-inserted safepoints.
func HostInterruptSupported() bool { return false }
