package cache

import (
	"errors"

	"github.com/wago-org/wago/cli/internal/tui"
	"github.com/wago-org/wago/internal/wagopaths"
)

const (
	downloadsSelection = "Downloads"
	buildsSelection    = "Builds"
)

// CleanPicker builds the cache cleanup selector with current sizes. Every
// regenerable cache category starts enabled so Enter cleans everything.
func CleanPicker(dirs wagopaths.Dirs) (*tui.MultiSelect, error) {
	downloadBytes, err := Size(Paths(dirs, Selection{Downloads: true}))
	if err != nil {
		return nil, err
	}
	buildBytes, err := Size(Paths(dirs, Selection{Builds: true}))
	if err != nil {
		return nil, err
	}
	return &tui.MultiSelect{
		Title:  "Choose caches to clean",
		Prompt: "↑/↓ move · space toggle · a toggle all · enter/→ clean · esc cancel",
		Items: []tui.SelectItem{
			{Label: downloadsSelection, Description: FormatBytes(downloadBytes), On: true},
			{Label: buildsSelection, Description: FormatBytes(buildBytes), On: true},
		},
	}, nil
}

// ChooseClean runs the interactive cache selector and converts its labels into
// the domain selection used by Clean.
func ChooseClean(dirs wagopaths.Dirs) (Selection, bool, error) {
	if !tui.StdinIsTTY() {
		return Selection{}, false, errors.New("interactive selection needs a terminal; pass --downloads, --builds, or --all with --yes")
	}
	picker, err := CleanPicker(dirs)
	if err != nil {
		return Selection{}, false, err
	}
	submitted, cancelled := tui.Run(picker)
	if !submitted || cancelled {
		return Selection{}, false, nil
	}
	selection := Selection{}
	for _, item := range picker.Items {
		if !item.On {
			continue
		}
		switch item.Label {
		case downloadsSelection:
			selection.Downloads = true
		case buildsSelection:
			selection.Builds = true
		}
	}
	return selection, true, nil
}
