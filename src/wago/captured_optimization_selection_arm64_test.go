// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone

package wago

import (
	"bytes"
	"slices"
	"testing"
)

func TestCapturedOptimizationSelectionPreservesConfig(t *testing.T) {
	const knob = "reg-abi"
	before := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	enabled := before.optimizations[knob]
	if !SetOptKnob(knob, !enabled) {
		t.Fatalf("missing %q option", knob)
	}
	t.Cleanup(func() { SetOptKnob(knob, enabled) })
	after := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)

	compile := func(cfg *RuntimeConfig) *Compiled {
		t.Helper()
		result, err := cfg.Compile(benchAddOneModule())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := result.Close(); err != nil {
				t.Error(err)
			}
		})
		return result
	}
	check := func(cfg *RuntimeConfig, want bool) *Compiled {
		t.Helper()
		captured := compile(cfg)
		uncaptured := *cfg
		uncaptured.trustedOptimizations = false
		fallback := compile(&uncaptured)
		if captured.registerABIDisabled == want ||
			!bytes.Equal(captured.code, fallback.code) ||
			!slices.Equal(captured.Entry, fallback.Entry) ||
			!slices.Equal(captured.InternalEntry, fallback.InternalEntry) {
			t.Fatalf("captured policy disagrees with name-based policy for %s=%t", knob, want)
		}
		return captured
	}
	old, latest := check(before, enabled), check(after, !enabled)
	if bytes.Equal(old.code, latest.code) && slices.Equal(old.InternalEntry, latest.InternalEntry) {
		t.Fatal("fixture did not exercise the changed register ABI")
	}
	override := compile(after.WithOptimization(knob, enabled))
	if !bytes.Equal(old.code, override.code) || !slices.Equal(old.InternalEntry, override.InternalEntry) {
		t.Fatal("an explicit override did not restore the original ABI")
	}
}
