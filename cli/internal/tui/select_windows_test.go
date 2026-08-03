//go:build windows

package tui

import (
	"bytes"
	"strings"
	"testing"
)

type fakeSelectorConsole struct {
	keys   []selectKey
	closed bool
}

func (c *fakeSelectorConsole) readKey() (selectKey, bool) {
	if len(c.keys) == 0 {
		return keyNoop, false
	}
	key := c.keys[0]
	c.keys = c.keys[1:]
	return key, true
}

func (c *fakeSelectorConsole) close() {
	c.closed = true
}

func TestRunUsesWindowsConsoleInput(t *testing.T) {
	console := &fakeSelectorConsole{keys: []selectKey{keyDown, keyAccept}}
	previousConsole, previousOutput := openSelectorConsole, selectorOutput
	var output bytes.Buffer
	openSelectorConsole = func() (selectorConsole, bool) {
		return console, true
	}
	selectorOutput = &output
	t.Cleanup(func() {
		openSelectorConsole = previousConsole
		selectorOutput = previousOutput
	})

	picker := NewPicker("Install Wago version", []Item{
		{Label: "canary", Value: "canary"},
		{Label: "nightly", Value: "nightly"},
	})
	submitted, cancelled := Run(picker)
	if !submitted || cancelled {
		t.Fatalf("Run() = submitted %t, cancelled %t; want true, false", submitted, cancelled)
	}
	if selected := picker.Selected(); selected != "nightly" {
		t.Fatalf("selected %q, want nightly", selected)
	}
	if !console.closed {
		t.Fatal("console was not restored")
	}
	if !strings.Contains(output.String(), "Install Wago version") {
		t.Fatalf("selector frame was not rendered:\n%q", output.String())
	}
}
