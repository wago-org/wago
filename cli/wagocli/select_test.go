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

func TestMultiSelectRejectRow(t *testing.T) {
	m := &multiSelect{
		items:  []selItem{{label: "a", on: true}, {label: "b", on: true}},
		reject: true,
	}
	m.apply(keyDown)
	m.apply(keyDown) // onto the Reject All row (index 2)
	if m.cursor != 2 {
		t.Fatalf("cursor should reach the Reject All row (2), got %d", m.cursor)
	}
	m.apply(keyDown) // clamps there
	if m.cursor != 2 {
		t.Fatalf("cursor should clamp at the Reject All row, got %d", m.cursor)
	}
	m.apply(keyToggle) // no-op on the reject row (must not panic or toggle an item)
	if got := m.chosen(); len(got) != 2 {
		t.Fatalf("toggle on reject row must not change item state, got %v", got)
	}
	done, cancelled := m.apply(keyAccept)
	if !done || cancelled || !m.rejected {
		t.Fatalf("enter on Reject All => done, rejected; got done=%v cancelled=%v rejected=%v", done, cancelled, m.rejected)
	}
}

func TestMultiSelectSubmitOnItemNotRejected(t *testing.T) {
	m := &multiSelect{
		items:  []selItem{{label: "a", on: true}, {label: "b", on: false}},
		reject: true,
	}
	done, cancelled := m.apply(keyAccept) // enter while on an item
	if !done || cancelled || m.rejected {
		t.Fatalf("enter on an item => submit; got done=%v cancelled=%v rejected=%v", done, cancelled, m.rejected)
	}
	if got := m.chosen(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("chosen=%v, want [a]", got)
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
