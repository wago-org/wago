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

func Interactive(config, reset settings.Config, scope string, startExperimental bool) (settings.Config, bool, error) {
	if !tui.StdinIsTTY() {
		return config, false, fmt.Errorf("interactive terminal required; use `wago config set <setting> <value>`")
	}
	changed := false
	if startExperimental {
		changed = chooseExperimental(&config)
	}
	for {
		selected, ok := tui.Choose("Configure Wago ("+scope+")", rootItems(config, scope))
		if !ok {
			break
		}
		var update bool
		var err error
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
			update = confirmReset(scope)
			if update {
				config = settings.Clone(reset)
			}
		}
		if err != nil {
			return config, changed, err
		}
		changed = changed || update
	}
	return config, changed, nil
}

func Print(w io.Writer, config settings.Config, includeExperimental bool, scope, path string, overrides []settings.Override) {
	overridden := make(map[string]bool, len(overrides))
	for _, override := range overrides {
		overridden[override.Key] = true
	}
	fmt.Fprintf(w, "%s %s\n", ui.Bold("Wago configuration"), ui.Dim("("+scope+")"))
	printValue(w, "location", path)
	fmt.Fprintln(w)
	fmt.Fprintln(w, ui.Bold("Runtime defaults"))
	printSetting(w, "parallel", parallelLabel(config.Runtime.Parallel), "runtime.parallel", overridden)
	printSetting(w, "deferred bounds checking", onOff(config.Runtime.DeferredBoundsChecking), "runtime.deferred-bounds-checking", overridden)
	fmt.Fprintln(w)
	fmt.Fprintln(w, ui.Bold("WebAssembly features"))
	for _, setting := range settings.Features() {
		printSetting(w, strings.TrimPrefix(setting.Key, "features."), onOff(config.Features[strings.TrimPrefix(setting.Key, "features.")]), setting.Key, overridden)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, ui.Bold("Compiler optimizations"))
	for _, setting := range stableOptimizations() {
		name := strings.TrimPrefix(setting.Key, "optimizations.")
		printSetting(w, name, onOff(config.Optimizations[name]), setting.Key, overridden)
	}
	if includeExperimental {
		fmt.Fprintln(w)
		fmt.Fprintln(w, ui.Bold("Experimental preview"))
		for _, setting := range settings.Experimental() {
			value := "unavailable"
			if setting.Available {
				value = onOff(booleanValue(config, setting))
			}
			printSetting(w, settingName(setting), value, setting.Key, overridden)
		}
	}
}

func PrintDiff(w io.Writer, scope string, overrides []settings.Override) {
	base := "built-in"
	if scope == settings.ScopeLocal {
		base = "global"
	}
	fmt.Fprintf(w, "%s %s\n", ui.Bold("Wago configuration differences"), ui.Dim("("+scope+")"))
	if len(overrides) == 0 {
		fmt.Fprintf(w, "No %s overrides.\n", scope)
		return
	}
	width := 0
	for _, override := range overrides {
		if len(override.Key) > width {
			width = len(override.Key)
		}
	}
	for _, override := range overrides {
		fmt.Fprintf(w, "  %-*s  %s %s  %s %s\n", width, override.Key, ui.Dim(base), override.Base, scope, override.Value)
	}
}

func rootItems(config settings.Config, scope string) []tui.Item {
	resetLabel, resetDescription := "Reset defaults", "restore built-in settings"
	if scope == settings.ScopeLocal {
		resetLabel, resetDescription = "Clear local overrides", "inherit global settings"
	}
	return []tui.Item{
		{Label: "WebAssembly features", Meta: fmt.Sprintf("%d enabled", enabledCount(config.Features)), Value: "features", Description: "accepted module features"},
		{Label: "Runtime defaults", Meta: parallelLabel(config.Runtime.Parallel), Value: "runtime", Description: "parallelism and bounds checks"},
		{Label: "Optimizations", Meta: fmt.Sprintf("%d enabled", enabledStableOptimizations(config)), Value: "optimizations", Description: "stable compiler defaults"},
		{Label: "Experimental preview", Meta: fmt.Sprintf("%d available", availableExperimental()), Value: "experimental", Description: "opt-in and planned features"},
		{Label: resetLabel, Value: "reset", Description: resetDescription},
	}
}

func chooseBooleans(title string, catalog []settings.BoolSetting, values map[string]bool) bool {
	items := make([]tui.SelectItem, 0, len(catalog))
	for _, setting := range catalog {
		name := setting.Key[strings.IndexByte(setting.Key, '.')+1:]
		items = append(items, tui.SelectItem{Label: setting.Label, Description: setting.Description, On: values[name]})
	}
	selector := &tui.MultiSelect{Title: title, Items: items, Prompt: "↑/↓ move · space toggle · a all · enter/→ save · ←/esc back"}
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
		items = append(items, tui.SelectItem{
			Label: setting.Label, Description: description, On: setting.Available && booleanValue(*config, setting), Disabled: !setting.Available,
		})
	}
	selector := &tui.MultiSelect{Title: "Experimental features (preview)", Items: items, Prompt: "↑/↓ move · space toggle available · enter/→ save · ←/esc back"}
	submitted, cancelled := tui.Run(selector)
	if !submitted || cancelled {
		return false
	}
	changed := false
	for index, setting := range catalog {
		if !setting.Available {
			continue
		}
		if booleanValue(*config, setting) != selector.Items[index].On {
			setBooleanValue(config, setting, selector.Items[index].On)
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

func confirmReset(scope string) bool {
	title, description := "Reset all global Wago defaults?", "restore every built-in default"
	if scope == settings.ScopeLocal {
		title, description = "Clear all local Wago overrides?", "inherit global settings"
	}
	selected, ok := tui.Choose(title, []tui.Item{
		{Label: "Yes", Value: "yes", Description: description},
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

func settingName(setting settings.BoolSetting) string {
	return setting.Key[strings.IndexByte(setting.Key, '.')+1:]
}

func booleanValue(config settings.Config, setting settings.BoolSetting) bool {
	name := settingName(setting)
	if strings.HasPrefix(setting.Key, "features.") {
		return config.Features[name]
	}
	return config.Optimizations[name]
}

func setBooleanValue(config *settings.Config, setting settings.BoolSetting, enabled bool) {
	name := settingName(setting)
	if strings.HasPrefix(setting.Key, "features.") {
		config.Features[name] = enabled
		return
	}
	config.Optimizations[name] = enabled
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

func printSetting(w io.Writer, name, value, key string, overridden map[string]bool) {
	if overridden[key] {
		value += "  " + ui.Dim("override")
	}
	printValue(w, name, value)
}
