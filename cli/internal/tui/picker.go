package tui

import (
	"fmt"
	"strings"

	"github.com/wago-org/wago/cli/internal/ui"
)

// pickerItem is one row in a single-choice picker. Rows with children can be
// opened with right arrow; value is returned when the row is accepted.
type Item struct {
	Label       string
	Meta        string
	MetaWidth   int
	Description string
	Value       string
	Children    []Item
	ChildCursor int
	AcceptTitle string
	AcceptItems []Item
}

type pickerPage struct {
	title  string
	items  []Item
	cursor int
}

// picker is a dependency-free hierarchical single-choice TUI. pages is its
// navigation stack; left or escape pops a nested page, while either key at the
// root and q at any depth cancel.
type Picker struct {
	pages []pickerPage
}

const pickerVisibleRows = 15

func NewPicker(title string, items []Item) *Picker {
	return &Picker{pages: []pickerPage{{title: title, items: items}}}
}

func (p *Picker) page() *pickerPage {
	return &p.pages[len(p.pages)-1]
}

func (p *Picker) apply(key selectKey) (done, cancelled bool) {
	page := p.page()
	switch key {
	case keyUp:
		if page.cursor > 0 {
			page.cursor--
		}
	case keyDown:
		if page.cursor < len(page.items)-1 {
			page.cursor++
		}
	case keyRight:
		if len(page.items) != 0 {
			item := page.items[page.cursor]
			if len(item.Children) != 0 {
				p.pages = append(p.pages, pickerPage{
					title:  page.title + " › " + item.Label,
					items:  item.Children,
					cursor: item.ChildCursor,
				})
			} else {
				return p.accept(item)
			}
		}
	case keyLeft:
		if !p.back() {
			return true, true
		}
	case keyCancel:
		if p.back() {
			return false, false
		}
		return true, true
	case keyQuit:
		return true, true
	case keyAccept:
		if len(page.items) != 0 {
			return p.accept(page.items[page.cursor])
		}
	}
	return false, false
}

func (p *Picker) accept(item Item) (done, cancelled bool) {
	if len(item.AcceptItems) != 0 {
		p.pages = append(p.pages, pickerPage{
			title: item.AcceptTitle,
			items: item.AcceptItems,
		})
		return false, false
	}
	if item.Value != "" {
		return true, false
	}
	return false, false
}

func (p *Picker) back() bool {
	if len(p.pages) <= 1 {
		return false
	}
	p.pages = p.pages[:len(p.pages)-1]
	return true
}

func (p *Picker) Selected() string {
	page := p.page()
	if len(page.items) == 0 {
		return ""
	}
	return page.items[page.cursor].Value
}

func (p *Picker) SetCursor(cursor int) {
	page := p.page()
	if cursor >= 0 && cursor < len(page.items) {
		page.cursor = cursor
	}
}

func (p *Picker) MoveDown() (done, cancelled bool) {
	return p.apply(keyDown)
}

func (p *Picker) SelectRight() (done, cancelled bool) {
	return p.apply(keyRight)
}

func (p *Picker) Apply(key Key) (done, cancelled bool) {
	return p.apply(key)
}

func (p *Picker) Depth() int {
	return len(p.pages)
}

func (p *Picker) frame() string {
	page := p.page()
	var b strings.Builder
	if page.title != "" {
		fmt.Fprintf(&b, "%s\n", ui.Bold(page.title))
	}
	labelW, metaW := 0, 0
	for _, item := range page.items {
		if len(item.Label) > labelW {
			labelW = len(item.Label)
		}
		if len(item.Meta) > metaW {
			metaW = len(item.Meta)
		}
		if item.MetaWidth > metaW {
			metaW = item.MetaWidth
		}
	}
	start, end := pickerWindow(page)
	if start > 0 {
		fmt.Fprintf(&b, "%s\n", ui.Dim(fmt.Sprintf("  ↑ %d more", start)))
	}
	for i := start; i < end; i++ {
		item := page.items[i]
		cursor, mark := "  ", "○"
		if i == page.cursor {
			cursor, mark = ui.Cyan("› "), ui.Cyan("◉")
		}
		line := fmt.Sprintf("%s%s %-*s", cursor, mark, labelW, item.Label)
		if metaW != 0 {
			line += fmt.Sprintf(" %-*s", metaW, item.Meta)
		}
		if len(item.Children) != 0 {
			gap := "  "
			if metaW != 0 {
				gap = " "
			}
			line += gap + ui.Dim("→")
		}
		if item.Description != "" {
			line += "  " + ui.Dim(item.Description)
		}
		fmt.Fprintf(&b, "%s\n", line)
	}
	if remaining := len(page.items) - end; remaining > 0 {
		fmt.Fprintf(&b, "%s\n", ui.Dim(fmt.Sprintf("  ↓ %d more", remaining)))
	}
	if len(p.pages) > 1 {
		fmt.Fprintf(&b, "%s\n", ui.Dim("↑/↓ move · enter/→ select · ←/esc back · q cancel"))
	} else if pickerHasChildren(page.items) {
		fmt.Fprintf(&b, "%s\n", ui.Dim("↑/↓ move · enter select · → select/browse · ←/esc cancel"))
	} else {
		fmt.Fprintf(&b, "%s\n", ui.Dim("↑/↓ move · enter/→ select · ←/esc cancel"))
	}
	return b.String()
}

func (p *Picker) Frame() string {
	return p.frame()
}

func pickerHasChildren(items []Item) bool {
	for _, item := range items {
		if len(item.Children) != 0 {
			return true
		}
	}
	return false
}

func pickerWindow(page *pickerPage) (start, end int) {
	if len(page.items) <= pickerVisibleRows {
		return 0, len(page.items)
	}
	start = (page.cursor / pickerVisibleRows) * pickerVisibleRows
	end = start + pickerVisibleRows
	if end > len(page.items) {
		end = len(page.items)
	}
	return start, end
}

func Choose(title string, items []Item) (string, bool) {
	if len(items) == 0 {
		return "", false
	}
	p := NewPicker(title, items)
	submitted, cancelled := Run(p)
	if !submitted || cancelled {
		return "", false
	}
	if selected := p.Selected(); selected != "" {
		return selected, true
	}
	return "", false
}
