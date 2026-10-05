//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestSourcePlanAdapterMetadataOwnershipAndHistory(t *testing.T) {
	for _, mode := range []string{"positive", "foreign event", "wrong cursor", "negative charge", "exhausted charge", "invalid failure", "closed"} {
		t.Run(mode, func(t *testing.T) {
			p, a := sourcePlanFixture(t, []byte{0x20, 0, 0x0b}, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, nil, SourcePlanLimits{})
			e := sourcePlanGetEvent(t, p, 0)
			beforeWork, beforeStorage := a.owner.sourceWork, a.owner.sourceStorage
			switch mode {
			case "positive":
				c, ok := SourcePlanEventContract(p, e)
				if !ok || c.Event.PC != 0 || c.Event.EndPC != 2 || c.Outputs[0].Type != wasm.I32 {
					t.Fatal(c, ok)
				}
				c.Event.Index = 99
				again, _ := SourcePlanEventContract(p, e)
				if again.Event.Index != 0 {
					t.Fatal("metadata alias")
				}
				n, _ := SourceEntryNode(p, 0)
				v, ok := SourceNodeContract(p, n)
				if !ok || v.Kind != wasm.SourceParameter {
					t.Fatal(v, ok)
				}
				if !ChargeSourcePlanAdapter(p, 3, 5) || a.owner.sourceWork >= beforeWork || a.owner.sourceStorage != beforeStorage-5 {
					t.Fatal("history charge")
				}
				return
			case "foreign event":
				q, _ := sourcePlanFixture(t, []byte{0x20, 0, 0x0b}, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, nil, SourcePlanLimits{})
				e = sourcePlanGetEvent(t, q, 0)
				_, _ = SourcePlanEventContract(p, e)
			case "wrong cursor":
				e = sourcePlanGetEvent(t, p, 1)
				_, _ = SourcePlanEventContract(p, e)
			case "negative charge":
				ChargeSourcePlanAdapter(p, -1, 0)
			case "exhausted charge":
				ChargeSourcePlanAdapter(p, 1<<30, 1<<30)
			case "invalid failure":
				FailSourcePlanAdapter(p, regalloccheck.NoFailure)
			case "closed":
				EndSourceAttempt(a)
				if _, ok := SourcePlanEventContract(p, e); ok || ChargeSourcePlanAdapter(p, 1, 1) {
					t.Fatal("closed token authority")
				}
				return
			}
			reason := SourcePlanStatus(p).Reason
			if reason == regalloccheck.NoFailure {
				t.Fatal("invalid adapter accepted")
			}
			FailSourcePlanAdapter(p, regalloccheck.UnsupportedOperation)
			if SourcePlanStatus(p).Reason != reason || ChargeSourcePlanAdapter(p, 0, 0) {
				t.Fatal("failure repaired")
			}
		})
	}
}
