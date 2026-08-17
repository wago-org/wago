package tui

import (
	"fmt"
	"strings"

	"github.com/wago-org/wago/cli/internal/ui"
)

// select.go is the dependency-free multi-select used for capability review.
// The model is pure (no terminal I/O); the shared raw-mode driver that feeds it
// keypresses and paints frames lives in select_unix.go / select_windows.go.

// selItem is one toggleable row.
type SelectItem struct {
	Label       string       // machine value, e.g. a capability id "wasi:stdio"
	Description string       // one-line human description (may be empty)
	Group       string       // optional section heading shared by adjacent rows
	On          bool         // currently selected
	Disabled    bool         // visible preview row that cannot be toggled or selected
	Fixed       bool         // visible checked row that is required and cannot be changed
	Action      bool         // visible action row that submits instead of toggling
	Children    []SelectItem // optional nested rows, expanded with →
	Expanded    bool
	Partial     bool // some, but not all, nested rows are enabled
	Reject      bool // exclusive visible action that clears every normal row
	ConfirmOff  bool // submit immediately when this selected row is toggled off
}

// selectKey is a normalized keypress the model understands.
type selectKey int

type Key = selectKey

const (
	keyNoop selectKey = iota // unrecognized / intentionally inert (e.g. ← →)
	keyUp
	keyDown
	keyToggle // space
	keyAll    // a toggles the whole list
	keyClear  // n
	keyAccept // enter / return — submit the checked items
	keyReject // r — clear everything and submit (grant nothing)
	keyCancel // esc — abort or navigate back
	keyQuit   // q / ctrl-c — abort from any page
	keyLeft
	keyRight
	keyCopy // c
	keyOpen // o
)

const (
	KeyUp     = keyUp
	KeyDown   = keyDown
	KeyToggle = keyToggle
	KeyAccept = keyAccept
	KeyCancel = keyCancel
	KeyLeft   = keyLeft
	KeyRight  = keyRight
)

// Action is the result of a one-key action prompt.
type Action int

const (
	ActionNone Action = iota
	ActionContinue
	ActionCopy
	ActionOpen
)

// ActionPrompt presents compact keyboard actions without a selectable list.
// The model remains pure; callers perform the selected side effect after Run.
type ActionPrompt struct {
	Text   string
	Prompt string
	action Action
}

func (p *ActionPrompt) apply(key selectKey) (done, cancelled bool) {
	switch key {
	case keyCopy:
		p.action = ActionCopy
		return true, false
	case keyOpen:
		p.action = ActionOpen
		return true, false
	case keyAccept, keyRight:
		p.action = ActionContinue
		return true, false
	case keyLeft, keyCancel, keyQuit:
		return true, true
	}
	return false, false
}

func (p *ActionPrompt) frame() string {
	prompt := p.Prompt
	if prompt == "" {
		prompt = "c copy code · o open browser · enter continue · esc cancel"
	}
	return p.Text + "\n" + ui.Dim(prompt) + "\n"
}

// Action reports the submitted action after Run returns.
func (p *ActionPrompt) Action() Action { return p.action }

// Frame returns the prompt's rendered text for tests and previews.
func (p *ActionPrompt) Frame() string { return p.frame() }

// selectorModel is implemented by both the capability multi-select and the
// hierarchical single-choice picker.
type selectorModel interface {
	apply(selectKey) (done, cancelled bool)
	frame() string
}

// MultiSelect is the pure picker state: a list plus a cursor. Prompt overrides
// the default footer hint. A Reject item and the r key both submit rejection.
type MultiSelect struct {
	Title  string
	Prompt string
	Items  []SelectItem
	Cursor int
	// DisableRejectShortcut keeps cancellation as an explicit visible action.
	DisableRejectShortcut bool
}

// apply advances the model by one key. It reports whether the interaction is
// finished, and if so whether it was cancelled (esc) rather than submitted.
// Enter submits the focused action, r clears then submits rejection, and
// movement clamps at the ends. On a nested row, → expands package details and
// ← collapses them (or returns to the parent).
func (m *MultiSelect) apply(k selectKey) (done, cancelled bool) {
	rows := m.rows()
	switch k {
	case keyUp:
		if m.Cursor > 0 {
			m.Cursor--
		}
	case keyDown:
		if m.Cursor < len(rows)-1 {
			m.Cursor++
		}
	case keyToggle:
		if len(rows) > 0 && !rows[m.Cursor].item.Disabled && !rows[m.Cursor].item.Fixed {
			item := rows[m.Cursor].item
			if item.Reject {
				m.rejectAll()
				return true, false
			} else if item.Action {
				return true, false
			} else {
				toggleSelectItem(item)
				m.syncTreeState()
				if item.On {
					m.clearRejection()
				} else if item.ConfirmOff {
					return true, false
				}
			}
		}
	case keyRight:
		if len(rows) > 0 && len(rows[m.Cursor].item.Children) != 0 {
			rows[m.Cursor].item.Expanded = true
		} else if !m.hasNestedItems() {
			return true, false
		}
	case keyLeft:
		if len(rows) > 0 {
			if rows[m.Cursor].item.Expanded {
				rows[m.Cursor].item.Expanded = false
			} else if rows[m.Cursor].parent != nil {
				for index, row := range m.rows() {
					if row.item == rows[m.Cursor].parent {
						m.Cursor = index
						break
					}
				}
			} else {
				return true, true
			}
		}
	case keyAll:
		allSelected, selectable := true, false
		for _, item := range m.Items {
			if item.Disabled || item.Fixed || item.Action || item.Reject {
				continue
			}
			selectable = true
			if !item.On {
				allSelected = false
				break
			}
		}
		allSelected = allSelected && selectable
		for i := range m.Items {
			if !m.Items[i].Disabled && !m.Items[i].Fixed && !m.Items[i].Action && !m.Items[i].Reject {
				m.Items[i].On = !allSelected
			} else if m.Items[i].Reject {
				m.Items[i].On = false
			}
		}
	case keyClear:
		for i := range m.Items {
			if !m.Items[i].Disabled && !m.Items[i].Fixed && !m.Items[i].Action {
				m.Items[i].On = false
			}
		}
	case keyReject: // clear all, then submit — a deliberate "grant nothing"
		if m.DisableRejectShortcut {
			return false, false
		}
		m.rejectAll()
		return true, false
	case keyAccept:
		if len(rows) > 0 && rows[m.Cursor].item.Reject && !rows[m.Cursor].item.Disabled {
			m.rejectAll()
		}
		return true, false
	case keyCancel, keyQuit:
		return true, true
	}
	return false, false
}

func (m *MultiSelect) hasNestedItems() bool {
	var nested func([]SelectItem) bool
	nested = func(items []SelectItem) bool {
		for index := range items {
			if len(items[index].Children) != 0 || nested(items[index].Children) {
				return true
			}
		}
		return false
	}
	return nested(m.Items)
}

type selectRow struct {
	item   *SelectItem
	parent *SelectItem
	depth  int
}

func (m *MultiSelect) rows() []selectRow {
	m.syncTreeState()
	rows := make([]selectRow, 0, len(m.Items))
	var appendRows func(items []SelectItem, parent *SelectItem, depth int)
	appendRows = func(items []SelectItem, parent *SelectItem, depth int) {
		for index := range items {
			item := &items[index]
			rows = append(rows, selectRow{item: item, parent: parent, depth: depth})
			if item.Expanded {
				appendRows(item.Children, item, depth+1)
			}
		}
	}
	appendRows(m.Items, nil, 0)
	return rows
}

func (m *MultiSelect) syncTreeState() {
	var sync func(item *SelectItem)
	sync = func(item *SelectItem) {
		if len(item.Children) == 0 {
			return
		}
		all, any := true, false
		for index := range item.Children {
			sync(&item.Children[index])
			child := item.Children[index]
			all = all && child.On
			any = any || child.On || child.Partial
		}
		item.On, item.Partial = all, any && !all
	}
	for index := range m.Items {
		sync(&m.Items[index])
	}
}

func toggleSelectItem(item *SelectItem) {
	if len(item.Children) == 0 {
		item.On = !item.On
		return
	}
	target := !item.On
	var apply func(*SelectItem)
	apply = func(child *SelectItem) {
		if !child.Fixed && !child.Disabled && !child.Action {
			child.On = target
		}
		for index := range child.Children {
			apply(&child.Children[index])
		}
	}
	for index := range item.Children {
		apply(&item.Children[index])
	}
}

func (m *MultiSelect) rejectAll() {
	for i := range m.Items {
		m.Items[i].On = m.Items[i].Reject
	}
}

// chosen returns the labels of the selected rows, in list order.
func (m *MultiSelect) Chosen() []string {
	var out []string
	for _, it := range m.Items {
		if it.On && !it.Disabled && !it.Reject {
			out = append(out, it.Label)
		}
	}
	return out
}

// Rejected reports whether the explicit reject-all action is selected.
func (m *MultiSelect) Rejected() bool {
	for _, item := range m.Items {
		if item.Reject && item.On {
			return true
		}
	}
	return false
}

func (m *MultiSelect) clearRejection() {
	for i := range m.Items {
		if m.Items[i].Reject {
			m.Items[i].On = false
		}
	}
}

// decodeKey maps a raw input chunk (one keypress, possibly a multi-byte escape
// sequence for arrows) to a selectKey. Kept pure and separate so key handling is
// testable without a terminal.
func decodeKey(b []byte) selectKey {
	switch {
	case len(b) == 0:
		return keyNoop
	case len(b) == 1:
		switch b[0] {
		case '\r', '\n':
			return keyAccept
		case ' ':
			return keyToggle
		case 'a', 'A':
			return keyAll
		case 'n', 'N':
			return keyClear
		case 'r', 'R':
			return keyReject
		case 'c', 'C':
			return keyCopy
		case 'o', 'O':
			return keyOpen
		case 27:
			return keyCancel
		case 'q', 'Q', 3:
			return keyQuit
		case 'k', 'K':
			return keyUp
		case 'j', 'J':
			return keyDown
		case 'h', 'H', '<':
			return keyLeft
		case 'l', 'L', '>':
			return keyRight
		}
	case len(b) >= 3 && b[0] == 27 && b[1] == '[':
		switch b[2] {
		case 'A':
			return keyUp
		case 'B':
			return keyDown
		case 'C':
			return keyRight
		case 'D':
			return keyLeft
		}
	}
	return keyNoop
}

// frame renders the selector as plain text (the driver repaints it each key):
// an optional title, radio-style selection marks, and a dim footer hint.
func (m *MultiSelect) frame() string {
	var b strings.Builder
	if m.Title != "" {
		fmt.Fprintf(&b, "%s\n", ui.Bold(m.Title))
	}
	rows := m.rows()
	start, end := multiSelectWindow(m, rows)
	if start > 0 {
		fmt.Fprintf(&b, "%s\n", ui.Dim(fmt.Sprintf("  ↑ %d more", start)))
	}
	previousGroup := ""
	for i := start; i < end; i++ {
		row := rows[i]
		it := *row.item
		if it.Group != "" && it.Group != previousGroup {
			fmt.Fprintf(&b, "%s\n", ui.Bold(it.Group))
		}
		previousGroup = it.Group
		indentDepth := row.depth
		if it.Group != "" && row.depth == 0 {
			indentDepth++
		}
		cursor := strings.Repeat("  ", indentDepth+1)
		if i == m.Cursor {
			cursor = ui.Cyan(strings.Repeat("  ", indentDepth) + "› ")
		}
		if it.Action {
			line := fmt.Sprintf("%s[ %s ]", cursor, it.Label)
			if it.Description != "" {
				line += "  " + ui.Dim(it.Description)
			}
			fmt.Fprintf(&b, "%s\n", line)
			continue
		}
		mark := "○"
		if it.Disabled {
			mark = ui.Dim("◌")
		} else if it.Fixed {
			mark = ui.Cyan("✓")
		} else if it.On {
			mark = ui.Cyan("◉")
		} else if it.Partial {
			mark = ui.Cyan("◐")
		} else if i == m.Cursor {
			mark = ui.Cyan(mark)
		}
		label := it.Label
		if it.Disabled {
			label = ui.Dim(label)
		}
		line := fmt.Sprintf("%s%s %s", cursor, mark, label)
		description := it.Description
		if len(it.Children) != 0 && description != "" {
			parts := strings.SplitN(description, "\n", 2)
			if len(line)+len(" · ")+len(parts[0]) <= 78 {
				line += " · " + ui.Dim(parts[0])
				if len(parts) == 2 {
					description = parts[1]
				} else {
					description = ""
				}
			}
		}
		fmt.Fprintf(&b, "%s\n", line)
		if description != "" {
			indent := strings.Repeat("  ", indentDepth+3)
			for _, line := range wrapSelectText(description, 78-len(indent)) {
				fmt.Fprintf(&b, "%s%s\n", indent, ui.Dim(line))
			}
		}
	}
	if remaining := len(rows) - end; remaining > 0 {
		fmt.Fprintf(&b, "%s\n", ui.Dim(fmt.Sprintf("  ↓ %d more", remaining)))
	}
	prompt := m.Prompt
	if prompt == "" {
		prompt = "↑/↓ move · space toggle · enter/→ accept · r reject all · ←/esc cancel"
	}
	fmt.Fprintf(&b, "%s\n", ui.Dim(prompt))
	return b.String()
}

func wrapSelectText(text string, width int) []string {
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		if width <= 0 || len(paragraph) <= width {
			lines = append(lines, paragraph)
			continue
		}
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			if len(line)+1+len(word) > width {
				lines = append(lines, line)
				line = word
				continue
			}
			line += " " + word
		}
		lines = append(lines, line)
	}
	return lines
}

func (m *MultiSelect) Frame() string {
	return m.frame()
}

func multiSelectWindow(m *MultiSelect, rows []selectRow) (start, end int) {
	if len(rows) <= pickerVisibleRows {
		return 0, len(rows)
	}
	start = (m.Cursor / pickerVisibleRows) * pickerVisibleRows
	end = start + pickerVisibleRows
	if end > len(rows) {
		end = len(rows)
	}
	return start, end
}
