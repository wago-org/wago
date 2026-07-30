package wagocli

import (
	"fmt"
	"strings"
	"testing"
)

func TestPickerDrillDownSelectAndBack(t *testing.T) {
	p := newPicker("Install Wago version", []pickerItem{
		{
			label: "canary",
			value: "canary",
			children: []pickerItem{
				{label: "canary-new", value: "canary-new"},
				{label: "canary-old", value: "canary-old"},
			},
		},
		{label: "latest", value: "latest"},
	})

	if frame := p.frame(); !strings.Contains(frame, "›") || !strings.Contains(frame, "◉") || !strings.Contains(frame, "○") || !strings.Contains(frame, "→") {
		t.Fatalf("root frame missing picker symbols:\n%s", frame)
	}
	if done, cancelled := p.apply(keyRight); done || cancelled || len(p.pages) != 2 {
		t.Fatalf("right drill-down = done %v, cancelled %v, pages %d", done, cancelled, len(p.pages))
	}
	p.apply(keyDown)
	if got := p.selected(); got != "canary-old" {
		t.Fatalf("nested selected = %q", got)
	}
	if done, cancelled := p.apply(keyCancel); done || cancelled || len(p.pages) != 1 {
		t.Fatalf("nested esc = done %v, cancelled %v, pages %d", done, cancelled, len(p.pages))
	}
	p.apply(keyRight)
	if done, cancelled := p.apply(keyLeft); done || cancelled || len(p.pages) != 1 {
		t.Fatalf("left back = done %v, cancelled %v, pages %d", done, cancelled, len(p.pages))
	}
	p.apply(keyRight)
	p.apply(keyDown)
	if done, cancelled := p.apply(keyAccept); !done || cancelled || p.selected() != "canary-old" {
		t.Fatalf("nested accept = done %v, cancelled %v, selected %q", done, cancelled, p.selected())
	}
}

func TestPickerRootEscapeAndQuitCancel(t *testing.T) {
	for _, key := range []selectKey{keyCancel, keyQuit} {
		p := newPicker("Pick", []pickerItem{{label: "one", value: "one"}})
		if done, cancelled := p.apply(key); !done || !cancelled {
			t.Fatalf("key %d = done %v, cancelled %v", key, done, cancelled)
		}
	}
}

func TestPickerAcceptPageReturnsWithLeftOrEscape(t *testing.T) {
	p := newPicker("Install Wago version", []pickerItem{{
		label:       "canary",
		value:       "canary",
		acceptTitle: "Choose Wago profile",
		acceptItems: []pickerItem{
			{label: "Standard", value: "canary\x00standard"},
			{label: "Lite", value: "canary\x00lite"},
			{label: "Minimal", value: "canary\x00minimal"},
		},
	}})

	if done, cancelled := p.apply(keyAccept); done || cancelled || len(p.pages) != 2 {
		t.Fatalf("accept page = done %v, cancelled %v, pages %d", done, cancelled, len(p.pages))
	}
	if frame := p.frame(); !strings.Contains(frame, "Choose Wago profile") || !strings.Contains(frame, "←/esc back") {
		t.Fatalf("profile frame missing back navigation:\n%s", frame)
	}
	if done, cancelled := p.apply(keyLeft); done || cancelled || len(p.pages) != 1 {
		t.Fatalf("left back = done %v, cancelled %v, pages %d", done, cancelled, len(p.pages))
	}
	p.apply(keyAccept)
	if done, cancelled := p.apply(keyCancel); done || cancelled || len(p.pages) != 1 {
		t.Fatalf("escape back = done %v, cancelled %v, pages %d", done, cancelled, len(p.pages))
	}
	p.apply(keyAccept)
	p.apply(keyDown)
	if done, cancelled := p.apply(keyAccept); !done || cancelled || p.selected() != "canary\x00lite" {
		t.Fatalf("profile accept = done %v, cancelled %v, selected %q", done, cancelled, p.selected())
	}
}

func TestPickerRightSelectsLeaf(t *testing.T) {
	p := newPicker("Choose Wago profile", []pickerItem{
		{label: "Standard", value: "standard"},
		{label: "Lite", value: "lite"},
	})
	p.apply(keyDown)
	if done, cancelled := p.apply(keyRight); !done || cancelled || p.selected() != "lite" {
		t.Fatalf("right select = done %v, cancelled %v, selected %q", done, cancelled, p.selected())
	}
}

func TestPickerFramePaginatesAtWindowBoundary(t *testing.T) {
	items := make([]pickerItem, 25)
	for i := range items {
		label := fmt.Sprintf("item-%02d", i)
		items[i] = pickerItem{label: label, value: label}
	}
	p := newPicker("Pick", items)
	for range 14 {
		p.apply(keyDown)
	}
	frame := p.frame()
	if !strings.Contains(frame, "item-00") || !strings.Contains(frame, "item-14") || strings.Contains(frame, "item-15") {
		t.Fatalf("picker changed page before crossing the boundary:\n%s", frame)
	}
	if strings.Contains(frame, "  ↑ ") || !strings.Contains(frame, "  ↓ 10 more") {
		t.Fatalf("first page overflow indicators are wrong:\n%s", frame)
	}
	if rows := strings.Count(frame, "○") + strings.Count(frame, "◉"); rows != pickerVisibleRows {
		t.Fatalf("picker rendered %d item rows, want %d:\n%s", rows, pickerVisibleRows, frame)
	}

	p.apply(keyDown)
	frame = p.frame()
	if strings.Contains(frame, "item-14") || !strings.Contains(frame, "item-15") || !strings.Contains(frame, "item-24") {
		t.Fatalf("picker did not switch pages at the boundary:\n%s", frame)
	}
	if !strings.Contains(frame, "  ↑ 15 more") || strings.Contains(frame, "  ↓ ") {
		t.Fatalf("second page overflow indicators are wrong:\n%s", frame)
	}
	if rows := strings.Count(frame, "○") + strings.Count(frame, "◉"); rows != 10 {
		t.Fatalf("last page rendered %d item rows, want 10:\n%s", rows, frame)
	}

	p.apply(keyUp)
	frame = p.frame()
	if !strings.Contains(frame, "item-14") || strings.Contains(frame, "item-15") {
		t.Fatalf("picker did not return to the previous page:\n%s", frame)
	}
}
