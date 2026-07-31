// Package pluginmenu builds the shared hierarchical package selector used by
// manager and runtime plugin commands.
package pluginmenu

import (
	"sort"
	"strings"

	"github.com/wago-org/wago/cli/internal/tui"
)

type Package struct {
	Name    string
	Version string
}

func Picker(title string, packages []Package) *tui.Picker {
	packages = append([]Package(nil), packages...)
	sort.Slice(packages, func(left, right int) bool { return packages[left].Name < packages[right].Name })

	type group struct {
		root     Package
		children []Package
	}
	groups := map[string]*group{}
	var roots []string
	for _, current := range packages {
		root := Root(current.Name)
		if groups[root] == nil {
			groups[root] = &group{}
			roots = append(roots, root)
		}
		if current.Name == root {
			groups[root].root = current
		} else {
			groups[root].children = append(groups[root].children, current)
		}
	}

	items := make([]tui.Item, 0, len(roots))
	for _, root := range roots {
		current := groups[root]
		item := tui.Item{
			Label:       root,
			Description: current.root.Version,
			Value:       current.root.Name,
		}
		for _, child := range current.children {
			item.Children = append(item.Children, tui.Item{
				Label:       ChildLabel(root, child.Name),
				Description: child.Version,
				Value:       child.Name,
			})
		}
		items = append(items, item)
	}
	return tui.NewPicker(title, items)
}

func Select(title string, packages []Package) (string, bool) {
	if len(packages) == 0 {
		return "", false
	}
	picker := Picker(title, packages)
	submitted, cancelled := tui.Run(picker)
	if !submitted || cancelled {
		return "", false
	}
	selected := picker.Selected()
	return selected, selected != ""
}

func Root(name string) string {
	first := strings.IndexByte(name, '/')
	if first < 0 {
		return name
	}
	second := strings.IndexByte(name[first+1:], '/')
	if second < 0 {
		return name
	}
	return name[:first+1+second]
}

func ChildLabel(root, name string) string {
	if !strings.HasPrefix(name, root+"/") {
		return name
	}
	repository := root
	if slash := strings.LastIndexByte(root, '/'); slash >= 0 {
		repository = root[slash+1:]
	}
	return repository + strings.TrimPrefix(name, root)
}
