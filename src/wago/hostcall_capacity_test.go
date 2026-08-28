package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func TestGCSyncHostSlotCapacityRejectsU16Overflow(t *testing.T) {
	fields := make([]gc.FieldDesc, 1<<15)
	for i := range fields {
		fields[i].Kind = gc.StorageV128
	}
	_, err := gcSyncHostSlotCapacity([]gc.TypeDesc{{Kind: gc.KindStruct, Fields: fields}})
	if err == nil || !strings.Contains(err.Error(), "uint16") {
		t.Fatalf("u16 overflow error = %v", err)
	}
}
