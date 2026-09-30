package shared

import (
	"reflect"
	"testing"
	"unsafe"
)

func TestLocalEventTapeIsCompactBoundedScratch(t *testing.T) {
	typ := reflect.TypeOf(LocalEvent{})
	for i := 0; i < typ.NumField(); i++ {
		switch typ.Field(i).Type.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.String:
			t.Fatalf("field %s has pointer-bearing kind %s", typ.Field(i).Name, typ.Field(i).Type.Kind())
		}
	}
	if got := unsafe.Sizeof(LocalEvent{}); got != 4 {
		t.Fatalf("event size = %d, want 4", got)
	}

	var tape LocalEventTape
	tape.Reset(2)
	tape.Append(LocalEventRead, 7, 3)
	tape.Append(LocalEventDefine, 7, 300)
	tape.Append(LocalEventCall, NoLocal, 0)
	if got := len(tape.Events); got != 2 {
		t.Fatalf("retained events = %d, want 2", got)
	}
	if !tape.Overflow {
		t.Fatal("tape did not report overflow")
	}
	if got := tape.Events[1].Depth; got != 255 {
		t.Fatalf("saturated depth = %d, want 255", got)
	}

	tape.Reset(2)
	if len(tape.Events) != 0 || tape.Overflow {
		t.Fatalf("reset tape = len %d overflow %v", len(tape.Events), tape.Overflow)
	}
}

func TestFindLocalEventMeta(t *testing.T) {
	pairs := []uint32{4, 11, 9, 22, 20, 33}
	for _, tc := range []struct {
		key, want uint32
	}{{4, 11}, {9, 22}, {20, 33}, {0, 0}, {10, 0}, {21, 0}} {
		if got := FindLocalEventMeta(pairs, tc.key); got != tc.want {
			t.Fatalf("key %d = %d, want %d", tc.key, got, tc.want)
		}
	}
}
