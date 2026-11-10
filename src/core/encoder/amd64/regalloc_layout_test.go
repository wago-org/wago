//go:build !wago_regalloccheck && !tinygo

package amd64

import (
	"fmt"
	"reflect"
	"testing"
)

func TestRegallocCheckOrdinaryLayout(t *testing.T) {
	original := reflect.TypeOf(Asm{})
	fields := make([]reflect.StructField, 0, original.NumField()-1)
	var originalOffsets []uintptr
	for i := 0; i < original.NumField(); i++ {
		field := original.Field(i)
		if field.Name == "regallocState" {
			if field.Type.Size() != 0 {
				t.Fatalf("checker state consumes %d bytes", field.Type.Size())
			}
			continue
		}
		originalOffsets = append(originalOffsets, field.Offset)
		fields = append(fields, reflect.StructField{Name: fmt.Sprintf("Field%d", i), Type: field.Type})
	}
	without := reflect.StructOf(fields)
	if original.Size() != without.Size() || original.Align() != without.Align() {
		t.Fatalf("checker changed layout: size/alignment %d/%d, without %d/%d", original.Size(), original.Align(), without.Size(), without.Align())
	}
	for i, offset := range originalOffsets {
		if without.Field(i).Offset != offset {
			t.Fatalf("field %d offset changed", i)
		}
	}
}
