package wagocli

import (
	"reflect"
	"testing"
)

func newTestSelect() *multiSelect {
	return &multiSelect{
		title: "pick",
		items: []selItem{
			{label: "wasi:stdio", on: true},
			{label: "wasi:clock", on: false},
			{label: "env:args", on: true},
		},
	}
}

func TestMultiSelectMovementClamps(t *testing.T) {
	m := newTestSelect()
	m.apply(keyUp) // already at top
	if m.cursor != 0 {
		t.Fatalf("cursor should clamp at 0, got %d", m.cursor)
	}
	m.apply(keyDown)
	m.apply(keyDown)
	m.apply(keyDown) // past the end
	if m.cursor != 2 {
		t.Fatalf("cursor should clamp at last index 2, got %d", m.cursor)
	}
}

func TestMultiSelectToggleAllNone(t *testing.T) {
	m := newTestSelect()
	m.apply(keyDown)   // cursor -> wasi:clock
	m.apply(keyToggle) // turn it on
	if got := m.chosen(); !reflect.DeepEqual(got, []string{"wasi:stdio", "wasi:clock", "env:args"}) {
		t.Fatalf("after toggle: %v", got)
	}
	m.apply(keyClear)
	if got := m.chosen(); got != nil {
		t.Fatalf("keyClear should deselect all, got %v", got)
	}
	m.apply(keyAll)
	if got := m.chosen(); len(got) != 3 {
		t.Fatalf("keyAll should select all, got %v", got)
	}
}

func TestMultiSelectAcceptCancel(t *testing.T) {
	m := newTestSelect()
	if done, cancelled := m.apply(keyAccept); !done || cancelled {
		t.Fatalf("enter => done, not cancelled; got done=%v cancelled=%v", done, cancelled)
	}
	if done, cancelled := m.apply(keyCancel); !done || !cancelled {
		t.Fatalf("esc => done and cancelled; got done=%v cancelled=%v", done, cancelled)
	}
	if done, _ := m.apply(keyNoop); done {
		t.Fatalf("noop must not finish the interaction")
	}
}

func TestMultiSelectRejectKey(t *testing.T) {
	m := &multiSelect{items: []selItem{{label: "a", on: true}, {label: "b", on: true}}}
	// r clears everything and submits (grant nothing), and is NOT a cancel.
	done, cancelled := m.apply(keyReject)
	if !done || cancelled {
		t.Fatalf("r => done, not cancelled; got done=%v cancelled=%v", done, cancelled)
	}
	if got := m.chosen(); len(got) != 0 {
		t.Fatalf("r must clear all selections, got %v", got)
	}
}

func TestMultiSelectEnterAccepts(t *testing.T) {
	m := &multiSelect{items: []selItem{{label: "a", on: true}, {label: "b", on: false}}}
	// Enter always submits the checked items (never rejects), wherever the cursor is.
	m.apply(keyDown) // cursor on the unchecked "b"
	done, cancelled := m.apply(keyAccept)
	if !done || cancelled {
		t.Fatalf("enter => submit, not cancel; got done=%v cancelled=%v", done, cancelled)
	}
	if got := m.chosen(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("chosen=%v, want [a]", got)
	}
}

func TestMultiSelectEscCancels(t *testing.T) {
	m := &multiSelect{items: []selItem{{label: "a", on: true}}}
	if done, cancelled := m.apply(keyCancel); !done || !cancelled {
		t.Fatalf("esc => done + cancelled; got done=%v cancelled=%v", done, cancelled)
	}
}

func TestDecodeKey(t *testing.T) {
	cases := []struct {
		in   []byte
		want selectKey
	}{
		{[]byte{'\r'}, keyAccept},
		{[]byte{'\n'}, keyAccept},
		{[]byte{' '}, keyToggle},
		{[]byte{'a'}, keyAll},
		{[]byte{'n'}, keyClear},
		{[]byte{'r'}, keyReject},
		{[]byte{'q'}, keyCancel},
		{[]byte{3}, keyCancel},  // Ctrl-C
		{[]byte{27}, keyCancel}, // bare ESC
		{[]byte{'j'}, keyDown},
		{[]byte{'k'}, keyUp},
		{[]byte{27, '[', 'A'}, keyUp},
		{[]byte{27, '[', 'B'}, keyDown},
		{[]byte{27, '[', 'C'}, keyNoop}, // → inert
		{[]byte{27, '[', 'D'}, keyNoop}, // ← inert
		{[]byte{'x'}, keyNoop},
		{nil, keyNoop},
	}
	for _, tc := range cases {
		if got := decodeKey(tc.in); got != tc.want {
			t.Errorf("decodeKey(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
