package wagocli

import (
	"fmt"
	"reflect"
	"strings"
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

func TestMultiSelectFrame(t *testing.T) {
	m := newTestSelect()
	text := m.frame()
	for _, want := range []string{"pick", "› ", "◉", "○", "wasi:stdio", "enter/→ accept"} {
		if !strings.Contains(text, want) {
			t.Fatalf("frame missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "[x]") || strings.Contains(text, "[ ]") {
		t.Fatalf("frame uses legacy checkbox glyphs:\n%s", text)
	}
	m.title = ""
	m.prompt = "custom prompt"
	m.items = []selItem{{label: "x", desc: "description"}}
	if text := m.frame(); strings.Contains(text, "pick") || !strings.Contains(text, "description") || !strings.Contains(text, "custom prompt") {
		t.Fatalf("custom frame = %q", text)
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
		{[]byte{'q'}, keyQuit},
		{[]byte{3}, keyQuit},    // Ctrl-C
		{[]byte{27}, keyCancel}, // bare ESC
		{[]byte{'j'}, keyDown},
		{[]byte{'k'}, keyUp},
		{[]byte{'h'}, keyLeft},
		{[]byte{'<'}, keyLeft},
		{[]byte{'l'}, keyRight},
		{[]byte{'>'}, keyRight},
		{[]byte{27, '[', 'A'}, keyUp},
		{[]byte{27, '[', 'B'}, keyDown},
		{[]byte{27, '[', 'C'}, keyRight},
		{[]byte{27, '[', 'D'}, keyLeft},
		{[]byte{'x'}, keyNoop},
		{nil, keyNoop},
	}
	for _, tc := range cases {
		if got := decodeKey(tc.in); got != tc.want {
			t.Errorf("decodeKey(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestMultiSelectRightSubmitsAndAllSelectsEveryItem(t *testing.T) {
	m := &multiSelect{items: []selItem{{label: "canary"}, {label: "nightly"}}}
	if done, cancelled := m.apply(keyAll); done || cancelled {
		t.Fatalf("select all = done %v, cancelled %v", done, cancelled)
	}
	if got := strings.Join(m.chosen(), ","); got != "canary,nightly" {
		t.Fatalf("chosen after all = %q", got)
	}
	if done, cancelled := m.apply(keyRight); !done || cancelled {
		t.Fatalf("right submit = done %v, cancelled %v", done, cancelled)
	}
}

func TestMultiSelectPaginatesAtWindowBoundary(t *testing.T) {
	items := make([]selItem, pickerVisibleRows+2)
	for i := range items {
		items[i].label = fmt.Sprintf("version-%02d", i)
	}
	m := &multiSelect{title: "Versions", items: items}
	for range pickerVisibleRows - 1 {
		m.apply(keyDown)
	}
	if frame := m.frame(); !strings.Contains(frame, "version-00") || strings.Contains(frame, "version-15") {
		t.Fatalf("multi-select changed page early:\n%s", frame)
	}
	m.apply(keyDown)
	if frame := m.frame(); strings.Contains(frame, "version-14") || !strings.Contains(frame, "version-15") || !strings.Contains(frame, "↑ 15 more") {
		t.Fatalf("multi-select did not page at boundary:\n%s", frame)
	}
}
