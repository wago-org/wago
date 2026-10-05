//go:build amd64 && wago_regalloccheck

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// Pointer keys locate associations only; source identity and lifetime come from
// the plan's owner-bound logical slots and fresh generations.
type nativeSourceAssociation struct {
	slot uint32
	ref  shared.SourceSlotRef
}
type nativeSourcePlanState struct {
	owner                     *shared.IntegerSourcePlan
	locals                    []shared.SourceNodeRef
	associations              map[*elem]nativeSourceAssociation
	nextSlot                  uint32
	nextEvent                 int
	pending, selected, sealed bool
	contract                  shared.SourceEventContract
	recipe                    shared.SourceRecipeToken
	rule                      shared.SourceRecipeRule
	start, count              int
	roots                     [128]*elem
	nodes                     [128]shared.SourceNodeRef
}
