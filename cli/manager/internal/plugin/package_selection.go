package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/tui"
	"github.com/wago-org/wago/cli/manager/internal/registry"
)

type packageInstallPrompt struct {
	index      int
	constraint string
	pkg        registry.InstallPackage
}

func findPackageInstallPrompts(ctx context.Context, specs []string) ([]packageInstallPrompt, error) {
	var prompts []packageInstallPrompt
	for index, spec := range specs {
		id, constraint, err := parsePluginSpec(spec)
		if err != nil {
			return nil, err
		}
		pkg, found, err := registry.ResolveInstallPackage(ctx, id)
		if err != nil {
			return nil, err
		}
		if found && len(pkg.Subpackages) != 0 {
			prompts = append(prompts, packageInstallPrompt{index: index, constraint: constraint, pkg: pkg})
		}
	}
	return prompts, nil
}

func reviewPackageInstallChoices(specs []string, prompts []packageInstallPrompt) ([]string, error) {
	if automation.NoInput() || len(prompts) == 0 {
		return specs, nil
	}
	selected := append([]string(nil), specs...)
	offset := 0
	for _, prompt := range prompts {
		modules, err := choosePackageInstall(prompt.pkg)
		if err != nil {
			return nil, err
		}
		position := prompt.index + offset
		selected = replacePackageInstallSpec(selected, position, modules, prompt.constraint)
		offset += len(modules) - 1
	}
	return selected, nil
}

func replacePackageInstallSpec(specs []string, position int, modules []string, constraint string) []string {
	replacements := make([]string, len(modules))
	for index, module := range modules {
		replacements[index] = pluginSpec(module, constraint)
	}
	return append(specs[:position], append(replacements, specs[position+1:]...)...)
}

func choosePackageInstall(pkg registry.InstallPackage) ([]string, error) {
	for {
		mode, ok := tui.Choose("Install "+pkg.Name, packageInstallModeItems(len(pkg.Subpackages)))
		if !ok {
			return nil, fmt.Errorf("package selection cancelled; no changes were made")
		}
		if mode == "all" {
			return []string{pkg.Module}, nil
		}
		selected, back, err := choosePackageProviders(pkg)
		if err != nil {
			return nil, err
		}
		if !back {
			return selected, nil
		}
	}
}

func packageInstallModeItems(count int) []tui.Item {
	return []tui.Item{
		{Label: "Everything", Description: fmt.Sprintf("install all %d providers", count), Value: "all"},
		{Label: "Choose providers", Description: "select only what you need", Value: "custom"},
	}
}

func choosePackageProviders(pkg registry.InstallPackage) ([]string, bool, error) {
	selector, modules := packageProviderSelector(pkg)
	for {
		submitted, cancelled := tui.Run(selector)
		if cancelled || !submitted {
			return nil, true, nil
		}
		selected := selectedPackageProviders(selector, modules)
		if !selector.Rejected() && len(selected) != 0 {
			return selected, false, nil
		}
		choice, ok := tui.Choose("Exit installation?", authorityExitItems())
		if ok && choice == "cancel" {
			return nil, false, fmt.Errorf("package selection cancelled; no changes were made")
		}
		for index := range selector.Items {
			if selector.Items[index].Reject {
				selector.Items[index].On = false
			}
		}
	}
}

func packageProviderSelector(pkg registry.InstallPackage) (*tui.MultiSelect, []string) {
	items := make([]tui.SelectItem, 0, len(pkg.Subpackages)+1)
	modules := make([]string, 0, len(pkg.Subpackages))
	for _, subpackage := range pkg.Subpackages {
		label := subpackage.Name
		if label == "" {
			label = strings.TrimPrefix(subpackage.Module, pkg.Module+"/")
		}
		description := subpackage.Description
		if subpackage.Stability != "" {
			description = "(" + subpackage.Stability + ") " + description
		}
		items = append(items, tui.SelectItem{Label: label, Description: strings.TrimSpace(description), On: true})
		modules = append(modules, subpackage.Module)
	}
	items = append(items, tui.SelectItem{Label: "Install none", Description: "cancel installation", Reject: true})
	return &tui.MultiSelect{
		Title:  "Providers · " + pkg.Name,
		Prompt: "space toggle · enter confirm · r install none · esc back",
		Items:  items,
	}, modules
}

func selectedPackageProviders(selector *tui.MultiSelect, modules []string) []string {
	var selected []string
	for index, module := range modules {
		if selector.Items[index].On {
			selected = append(selected, module)
		}
	}
	return selected
}

func pluginSpec(id, constraint string) string {
	if constraint == "" || constraint == "*" {
		return id
	}
	return id + "@" + constraint
}
