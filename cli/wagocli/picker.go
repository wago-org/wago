package wagocli

import (
	"fmt"
	"strings"
)

// pickerItem is one row in a single-choice picker. Rows with children can be
// opened with right arrow; value is returned when the row is accepted.
type pickerItem struct {
	label       string
	meta        string
	metaWidth   int
	desc        string
	value       string
	children    []pickerItem
	childCursor int
	acceptTitle string
	acceptItems []pickerItem
}

type pickerPage struct {
	title  string
	items  []pickerItem
	cursor int
}

// picker is a dependency-free hierarchical single-choice TUI. pages is its
// navigation stack; left or escape pops a nested page, while escape at the root
// and q at any depth cancel.
type picker struct {
	pages []pickerPage
}

const pickerVisibleRows = 15

func newPicker(title string, items []pickerItem) *picker {
	return &picker{pages: []pickerPage{{title: title, items: items}}}
}

func (p *picker) page() *pickerPage {
	return &p.pages[len(p.pages)-1]
}

func (p *picker) apply(key selectKey) (done, cancelled bool) {
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
			if len(item.children) != 0 {
				p.pages = append(p.pages, pickerPage{
					title:  page.title + " › " + item.label,
					items:  item.children,
					cursor: item.childCursor,
				})
			} else {
				return p.accept(item)
			}
		}
	case keyLeft:
		p.back()
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

func (p *picker) accept(item pickerItem) (done, cancelled bool) {
	if len(item.acceptItems) != 0 {
		p.pages = append(p.pages, pickerPage{
			title: item.acceptTitle,
			items: item.acceptItems,
		})
		return false, false
	}
	if item.value != "" {
		return true, false
	}
	return false, false
}

func (p *picker) back() bool {
	if len(p.pages) <= 1 {
		return false
	}
	p.pages = p.pages[:len(p.pages)-1]
	return true
}

func (p *picker) selected() string {
	page := p.page()
	if len(page.items) == 0 {
		return ""
	}
	return page.items[page.cursor].value
}

func (p *picker) frame() string {
	page := p.page()
	var b strings.Builder
	if page.title != "" {
		fmt.Fprintf(&b, "%s\n", bold(page.title))
	}
	labelW, metaW := 0, 0
	for _, item := range page.items {
		if len(item.label) > labelW {
			labelW = len(item.label)
		}
		if len(item.meta) > metaW {
			metaW = len(item.meta)
		}
		if item.metaWidth > metaW {
			metaW = item.metaWidth
		}
	}
	start, end := pickerWindow(page)
	if start > 0 {
		fmt.Fprintf(&b, "%s\n", dim(fmt.Sprintf("  ↑ %d more", start)))
	}
	for i := start; i < end; i++ {
		item := page.items[i]
		cursor, mark := "  ", "○"
		if i == page.cursor {
			cursor, mark = cyan("› "), cyan("◉")
		}
		line := fmt.Sprintf("%s%s %-*s", cursor, mark, labelW, item.label)
		if metaW != 0 {
			line += fmt.Sprintf(" %-*s", metaW, item.meta)
		}
		if len(item.children) != 0 {
			gap := "  "
			if metaW != 0 {
				gap = " "
			}
			line += gap + dim("→")
		}
		if item.desc != "" {
			line += "  " + dim(item.desc)
		}
		fmt.Fprintf(&b, "%s\n", line)
	}
	if remaining := len(page.items) - end; remaining > 0 {
		fmt.Fprintf(&b, "%s\n", dim(fmt.Sprintf("  ↓ %d more", remaining)))
	}
	if len(p.pages) > 1 {
		fmt.Fprintf(&b, "%s\n", dim("↑/↓ move · enter/→ select · ←/esc back · q cancel"))
	} else if pickerHasChildren(page.items) {
		fmt.Fprintf(&b, "%s\n", dim("↑/↓ move · enter select · → select/browse · esc cancel"))
	} else {
		fmt.Fprintf(&b, "%s\n", dim("↑/↓ move · enter/→ select · esc cancel"))
	}
	return b.String()
}

func pickerHasChildren(items []pickerItem) bool {
	for _, item := range items {
		if len(item.children) != 0 {
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

func choosePicker(title string, items []pickerItem) (string, bool) {
	if len(items) == 0 {
		return "", false
	}
	p := newPicker(title, items)
	submitted, cancelled := runSelector(p)
	if !submitted || cancelled {
		return "", false
	}
	if selected := p.selected(); selected != "" {
		return selected, true
	}
	return "", false
}
