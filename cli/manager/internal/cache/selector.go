// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

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
	downloadBytes, err := Measure(dirs, Selection{Downloads: true})
	if err != nil {
		return nil, err
	}
	buildBytes, err := Measure(dirs, Selection{Builds: true})
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
		return Selection{}, false, errors.New("interactive selection needs a terminal; pass --downloads, --builds, or --all")
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
