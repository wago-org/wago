package tui

import (
	"fmt"
	"strings"
	"testing"
)

func TestPickerDrillDownSelectAndBack(t *testing.T) {
	p := NewPicker("Install Wago version", []Item{
		{
			Label: "canary",
			Value: "canary",
			Children: []Item{
				{Label: "canary-new", Value: "canary-new"},
				{Label: "canary-old", Value: "canary-old"},
			},
		},
		{Label: "latest", Value: "latest"},
	})

	if frame := p.frame(); !strings.Contains(frame, "›") || !strings.Contains(frame, "◉") || !strings.Contains(frame, "○") || !strings.Contains(frame, "→") {
		t.Fatalf("root frame missing picker symbols:\n%s", frame)
	}
	if done, cancelled := p.apply(keyRight); done || cancelled || len(p.pages) != 2 {
		t.Fatalf("right drill-down = done %v, cancelled %v, pages %d", done, cancelled, len(p.pages))
	}
	p.apply(keyDown)
	if got := p.Selected(); got != "canary-old" {
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
	if done, cancelled := p.apply(keyAccept); !done || cancelled || p.Selected() != "canary-old" {
		t.Fatalf("nested accept = done %v, cancelled %v, selected %q", done, cancelled, p.Selected())
	}
}

func TestPickerRootLeftEscapeAndQuitCancel(t *testing.T) {
	for _, key := range []selectKey{keyLeft, keyCancel, keyQuit} {
		p := NewPicker("Pick", []Item{{Label: "one", Value: "one"}})
		if done, cancelled := p.apply(key); !done || !cancelled {
			t.Fatalf("key %d = done %v, cancelled %v", key, done, cancelled)
		}
	}
}

func TestPickerAcceptPageReturnsWithLeftOrEscape(t *testing.T) {
	p := NewPicker("Install Wago version", []Item{{
		Label:       "canary",
		Value:       "canary",
		AcceptTitle: "Choose Wago profile",
		AcceptItems: []Item{
			{Label: "Standard", Value: "canary\x00standard"},
			{Label: "Minimal", Value: "canary\x00minimal"},
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
	if done, cancelled := p.apply(keyAccept); !done || cancelled || p.Selected() != "canary\x00minimal" {
		t.Fatalf("profile accept = done %v, cancelled %v, selected %q", done, cancelled, p.Selected())
	}
}

func TestPickerRightSelectsLeaf(t *testing.T) {
	p := NewPicker("Choose Wago profile", []Item{
		{Label: "Standard", Value: "standard"},
		{Label: "Minimal", Value: "minimal"},
	})
	p.apply(keyDown)
	if done, cancelled := p.apply(keyRight); !done || cancelled || p.Selected() != "minimal" {
		t.Fatalf("right select = done %v, cancelled %v, selected %q", done, cancelled, p.Selected())
	}
}

func TestPickerFramePaginatesAtWindowBoundary(t *testing.T) {
	items := make([]Item, 25)
	for i := range items {
		label := fmt.Sprintf("item-%02d", i)
		items[i] = Item{Label: label, Value: label}
	}
	p := NewPicker("Pick", items)
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
