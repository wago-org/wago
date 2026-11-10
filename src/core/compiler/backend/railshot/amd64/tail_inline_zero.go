// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

type tailInitFrame struct {
	entry, then, exit                      uint8
	entryLive, thenLive, exitLive, hasElse bool
	kind                                   byte
}

func mergeTailInit(a uint8, aLive bool, b uint8, bLive bool) (uint8, bool) {
	if !aLive {
		return b, bLive
	}
	if !bLive {
		return a, aLive
	}
	return a & b, true
}

// Determine which declared integer locals can be read before a definition on
// any reachable path. Source loops and unknown constructs retain all zeros.
// Both fallthrough and every structured branch exit participate in the merge.
func tailInlineZeroMask(body []byte, params, locals int, classifier wasm.ModuleInstructionClassifier) uint8 {
	if locals > 8 || params > locals || params < 0 {
		return 255
	}
	declared := uint8((1<<locals)-1) &^ uint8((1<<params)-1)
	var frames [64]tailInitFrame
	frames[0] = tailInitFrame{entryLive: true}
	depth := 1
	defined := uint8((1 << params) - 1)
	live := true
	need := uint8(0)
	r := wasm.ReaderFrom(body)
	record := func(label uint32) bool {
		if uint64(label) >= uint64(depth) {
			return false
		}
		frame := &frames[depth-1-int(label)]
		frame.exit, frame.exitLive = mergeTailInit(frame.exit, frame.exitLive, defined, live)
		return true
	}
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return declared
		}
		switch op {
		case 0x20, 0x21, 0x22:
			index, err := r.U32()
			if err != nil || int(index) >= locals {
				return declared
			}
			if live {
				bit := uint8(1 << index)
				if op == 0x20 {
					if defined&bit == 0 {
						need |= bit
					}
				} else {
					defined |= bit
				}
			}
		case 0x02, 0x04:
			if depth == len(frames) || classifier.SkipImmediate(&r, op) != nil {
				return declared
			}
			frames[depth] = tailInitFrame{kind: op, entry: defined, entryLive: live}
			depth++
		case 0x05:
			if depth <= 1 {
				return declared
			}
			frame := &frames[depth-1]
			if frame.kind != 0x04 || frame.hasElse {
				return declared
			}
			frame.then, frame.thenLive, frame.hasElse = defined, live, true
			defined, live = frame.entry, frame.entryLive
		case 0x0b:
			if depth == 1 {
				if r.HasNext() {
					return declared
				}
				return need & declared
			}
			depth--
			frame := frames[depth]
			if frame.kind == 0x04 {
				if frame.hasElse {
					defined, live = mergeTailInit(defined, live, frame.then, frame.thenLive)
				} else {
					defined, live = mergeTailInit(defined, live, frame.entry, frame.entryLive)
				}
			}
			defined, live = mergeTailInit(defined, live, frame.exit, frame.exitLive)
		case 0x0c, 0x0d:
			label, err := r.U32()
			if err != nil || !record(label) {
				return declared
			}
			if op == 0x0c {
				live = false
			}
		case 0x0e:
			count, err := r.U32()
			if err != nil || count > 64 {
				return declared
			}
			for i := uint32(0); i <= count; i++ {
				label, err := r.U32()
				if err != nil || !record(label) {
					return declared
				}
			}
			live = false
		case 0x12:
			if _, err := r.U32(); err != nil {
				return declared
			}
			live = false
		case 0x00, 0x0f:
			live = false
		default:
			ordinary := op == 0x01 || op >= 0x1a && op <= 0x1c || op >= 0x41 && op <= 0xc4
			if !ordinary || classifier.SkipImmediate(&r, op) != nil {
				return declared
			}
		}
	}
	return declared
}
