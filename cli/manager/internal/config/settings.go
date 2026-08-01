package config

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/wago-org/wago/cli/internal/settings"
	"github.com/wago-org/wago/cli/internal/tui"
	"github.com/wago-org/wago/cli/internal/ui"
)

func Interactive(startExperimental bool) (bool, error) {
	if !tui.StdinIsTTY() {
		return false, fmt.Errorf("interactive terminal required; use `wago config set <setting> <value>`")
	}
	config, err := settings.Load()
	if err != nil {
		return false, err
	}
	changed := false
	if startExperimental {
		changed = chooseExperimental(&config)
	}
	for {
		selected, ok := tui.Choose("Configure Wago", rootItems(config))
		if !ok {
			break
		}
		var update bool
		switch selected {
		case "features":
			update = chooseBooleans("WebAssembly features", settings.Features(), config.Features)
		case "runtime":
			update, err = chooseRuntime(&config)
		case "optimizations":
			update = chooseBooleans("Compiler optimizations", stableOptimizations(), config.Optimizations)
		case "experimental":
			update = chooseExperimental(&config)
		case "reset":
			update = confirmReset()
			if update {
				config = settings.Default()
			}
		}
		if err != nil {
			return changed, err
		}
		changed = changed || update
	}
	if changed {
		if err := settings.Save(config); err != nil {
			return false, err
		}
	}
	return changed, nil
}

func Print(w io.Writer, config settings.Config, includeExperimental bool) {
	fmt.Fprintln(w, ui.Bold("Wago configuration"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, ui.Bold("Runtime defaults"))
	printValue(w, "parallel", parallelLabel(config.Runtime.Parallel))
	printValue(w, "deferred bounds checking", onOff(config.Runtime.DeferredBoundsChecking))
	fmt.Fprintln(w)
	fmt.Fprintln(w, ui.Bold("WebAssembly features"))
	for _, setting := range settings.Features() {
		printValue(w, strings.TrimPrefix(setting.Key, "features."), onOff(config.Features[strings.TrimPrefix(setting.Key, "features.")]))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, ui.Bold("Compiler optimizations"))
	for _, setting := range stableOptimizations() {
		name := strings.TrimPrefix(setting.Key, "optimizations.")
		printValue(w, name, onOff(config.Optimizations[name]))
	}
	if includeExperimental {
		fmt.Fprintln(w)
		fmt.Fprintln(w, ui.Bold("Experimental preview"))
		for _, setting := range settings.Experimental() {
			value := "unavailable"
			if setting.Available {
				name := strings.TrimPrefix(setting.Key, "optimizations.")
				value = onOff(config.Optimizations[name])
			}
			printValue(w, strings.TrimPrefix(strings.TrimPrefix(setting.Key, "preview."), "optimizations."), value)
		}
	}
}

func rootItems(config settings.Config) []tui.Item {
	return []tui.Item{
		{Label: "WebAssembly features", Meta: fmt.Sprintf("%d enabled", enabledCount(config.Features)), Value: "features", Description: "accepted module features"},
		{Label: "Runtime defaults", Meta: parallelLabel(config.Runtime.Parallel), Value: "runtime", Description: "parallelism and bounds checks"},
		{Label: "Optimizations", Meta: fmt.Sprintf("%d enabled", enabledStableOptimizations(config)), Value: "optimizations", Description: "stable compiler defaults"},
		{Label: "Experimental preview", Meta: fmt.Sprintf("%d available", availableExperimental()), Value: "experimental", Description: "opt-in and planned features"},
		{Label: "Reset defaults", Value: "reset", Description: "restore built-in settings"},
	}
}

func chooseBooleans(title string, catalog []settings.BoolSetting, values map[string]bool) bool {
	items := make([]tui.SelectItem, 0, len(catalog))
	for _, setting := range catalog {
		name := setting.Key[strings.IndexByte(setting.Key, '.')+1:]
		items = append(items, tui.SelectItem{Label: setting.Label, Description: setting.Description, On: values[name]})
	}
	selector := &tui.MultiSelect{Title: title, Items: items, Prompt: "↑/↓ move · space toggle · a all · enter/→ save · esc back"}
	submitted, cancelled := tui.Run(selector)
	if !submitted || cancelled {
		return false
	}
	changed := false
	for index, setting := range catalog {
		name := setting.Key[strings.IndexByte(setting.Key, '.')+1:]
		if values[name] != selector.Items[index].On {
			values[name] = selector.Items[index].On
			changed = true
		}
	}
	return changed
}

func chooseExperimental(config *settings.Config) bool {
	catalog := settings.Experimental()
	items := make([]tui.SelectItem, 0, len(catalog))
	for _, setting := range catalog {
		description := setting.Description
		if !setting.Available {
			description += " · planned"
		}
		name := strings.TrimPrefix(setting.Key, "optimizations.")
		items = append(items, tui.SelectItem{
			Label: setting.Label, Description: description, On: setting.Available && config.Optimizations[name], Disabled: !setting.Available,
		})
	}
	selector := &tui.MultiSelect{Title: "Experimental features (preview)", Items: items, Prompt: "↑/↓ move · space toggle available · enter/→ save · esc back"}
	submitted, cancelled := tui.Run(selector)
	if !submitted || cancelled {
		return false
	}
	changed := false
	for index, setting := range catalog {
		if !setting.Available {
			continue
		}
		name := strings.TrimPrefix(setting.Key, "optimizations.")
		if config.Optimizations[name] != selector.Items[index].On {
			config.Optimizations[name] = selector.Items[index].On
			changed = true
		}
	}
	return changed
}

func chooseRuntime(config *settings.Config) (bool, error) {
	selected, ok := tui.Choose("Runtime defaults", []tui.Item{
		{Label: "Parallel compilation", Meta: parallelLabel(config.Runtime.Parallel), Value: "parallel", Description: "function validation and codegen"},
		{Label: "Deferred bounds checks", Meta: onOff(config.Runtime.DeferredBoundsChecking), Value: "bounds", Description: "skip checks already proven safe"},
	})
	if !ok {
		return false, nil
	}
	switch selected {
	case "parallel":
		value, ok := tui.Choose("Parallel compilation", []tui.Item{
			{Label: "Serial", Value: "1", Description: "lowest overhead for small modules"},
			{Label: "Adaptive", Value: "auto", Description: "scale workers with module size"},
			{Label: "2 workers", Value: "2"}, {Label: "4 workers", Value: "4"}, {Label: "8 workers", Value: "8"},
		})
		if !ok || value == config.Runtime.Parallel {
			return false, nil
		}
		config.Runtime.Parallel = value
		return true, nil
	case "bounds":
		value, ok := tui.Choose("Deferred bounds checks", []tui.Item{
			{Label: "On", Value: "true", Description: "skip checks already proven safe"},
			{Label: "Off", Value: "false", Description: "emit every explicit check"},
		})
		if !ok {
			return false, nil
		}
		enabled, err := strconv.ParseBool(value)
		if err != nil || enabled == config.Runtime.DeferredBoundsChecking {
			return false, err
		}
		config.Runtime.DeferredBoundsChecking = enabled
		return true, nil
	}
	return false, nil
}

func confirmReset() bool {
	selected, ok := tui.Choose("Reset all Wago defaults?", []tui.Item{
		{Label: "Yes", Value: "yes", Description: "restore every built-in default"},
		{Label: "No", Value: "no", Description: "keep current settings"},
	})
	return ok && selected == "yes"
}

func stableOptimizations() []settings.BoolSetting {
	var stable []settings.BoolSetting
	for _, setting := range settings.Optimizations() {
		if !setting.Experimental {
			stable = append(stable, setting)
		}
	}
	return stable
}

func enabledCount(values map[string]bool) int {
	count := 0
	for _, enabled := range values {
		if enabled {
			count++
		}
	}
	return count
}

func enabledStableOptimizations(config settings.Config) int {
	count := 0
	for _, setting := range stableOptimizations() {
		if config.Optimizations[strings.TrimPrefix(setting.Key, "optimizations.")] {
			count++
		}
	}
	return count
}

func availableExperimental() int {
	count := 0
	for _, setting := range settings.Experimental() {
		if setting.Available {
			count++
		}
	}
	return count
}

func parallelLabel(value string) string {
	switch value {
	case "1":
		return "serial"
	case "auto":
		return "adaptive"
	default:
		return value + " workers"
	}
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func printValue(w io.Writer, name, value string) {
	fmt.Fprintf(w, "  %-42s %s\n", name, value)
}
