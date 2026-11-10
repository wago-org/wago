// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_regalloccheck

package codegen

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"testing"
)

func TestSourceAccountingPreservesMachineAndQuotaResults(t *testing.T) {
	for _, previous := range []regalloccheck.Result{
		{Verdict: regalloccheck.Verified},
		{Verdict: regalloccheck.Rejected, Reason: regalloccheck.ProvenanceMismatch},
		{Verdict: regalloccheck.Inconclusive, Reason: regalloccheck.ResourceLimit},
		{Verdict: regalloccheck.Inconclusive, Reason: regalloccheck.UnsupportedOperation},
	} {
		ctx := &SourceContext{reporter: func(SourceReport) {}, reports: []regalloccheck.Result{previous}}
		candidate := regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: regalloccheck.InvalidGraph, Message: "source-only mismatch"}
		RecordSourceAccountingResult(ctx, 0, candidate)
		protected := previous.Verdict != regalloccheck.Inconclusive || previous.Reason == regalloccheck.ResourceLimit
		want := candidate
		if protected {
			want = previous
		}
		if ctx.reports[0] != want {
			t.Fatal(previous, ctx.reports[0])
		}
		RecordSourceAccountingResult(ctx, -1, candidate)
		RecordSourceAccountingResult(ctx, 1, candidate)
		if ctx.reports[0] != want {
			t.Fatal("foreign function replaced report")
		}
	}
}

func TestSourceAccountingCannotPromoteMachineVerdict(t *testing.T) {
	for _, candidate := range []regalloccheck.Result{{Verdict: regalloccheck.Verified}, {Verdict: regalloccheck.Rejected, Reason: regalloccheck.ProvenanceMismatch}} {
		previous := regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: regalloccheck.UnsupportedOperation, Message: "physical consumption pending"}
		ctx := &SourceContext{reporter: func(SourceReport) {}, reports: []regalloccheck.Result{previous}}
		RecordSourceAccountingResult(ctx, 0, candidate)
		if ctx.reports[0] != previous {
			t.Fatalf("source accounting promoted machine verdict: %+v", ctx.reports[0])
		}
	}
}
