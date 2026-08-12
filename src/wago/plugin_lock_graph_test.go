package wago

import (
	"strings"
	"testing"
)

func TestPluginPlanRequiresExactReviewedDependencyEdges(t *testing.T) {
	dependencyDefinition := testDefinition("example.com/reviewed/dependency")
	rootDefinition := testDefinition("example.com/reviewed/root")
	rootDefinition.Requires = []PluginRequirement{{ID: dependencyDefinition.ID, Version: "^1.0.0"}}
	provider := func(definition PluginDefinition) PluginProvider {
		return PluginProvider{Definition: definition, New: func() Plugin { return pluginFunc(nil) }}
	}

	set := testSet(t, provider(rootDefinition), provider(dependencyDefinition))
	for index := range set.Selections {
		set.Selections[index].Direct = set.Selections[index].ID == rootDefinition.ID
	}
	if _, err := InspectPluginPlan(set); err != nil {
		t.Fatalf("exact reviewed dependency graph: %v", err)
	}

	for name, mutate := range map[string]func(*PluginSelection){
		"missing": func(selection *PluginSelection) { delete(selection.Dependencies, dependencyDefinition.ID) },
		"changed constraint": func(selection *PluginSelection) {
			selection.Dependencies[dependencyDefinition.ID] = "*"
		},
		"extra": func(selection *PluginSelection) {
			selection.Dependencies["example.com/reviewed/extra"] = "^1.0.0"
		},
	} {
		t.Run(name, func(t *testing.T) {
			broken := testSet(t, provider(rootDefinition), provider(dependencyDefinition))
			for index := range broken.Selections {
				broken.Selections[index].Direct = broken.Selections[index].ID == rootDefinition.ID
				if broken.Selections[index].ID == rootDefinition.ID {
					mutate(&broken.Selections[index])
				}
			}
			if _, err := InspectPluginPlan(broken); err == nil || !strings.Contains(err.Error(), "dependencies") {
				t.Fatalf("tampered reviewed dependency graph error = %v", err)
			}
		})
	}
}

func TestPluginPlanRejectsSelectionUnreachableFromDirectRoots(t *testing.T) {
	rootDefinition := testDefinition("example.com/reachability/root")
	orphanDefinition := testDefinition("example.com/reachability/orphan")
	provider := func(definition PluginDefinition) PluginProvider {
		return PluginProvider{Definition: definition, New: func() Plugin { return pluginFunc(nil) }}
	}
	set := testSet(t, provider(rootDefinition), provider(orphanDefinition))
	for index := range set.Selections {
		set.Selections[index].Direct = set.Selections[index].ID == rootDefinition.ID
	}
	if _, err := InspectPluginPlan(set); err == nil || !strings.Contains(err.Error(), "unreachable from every reviewed direct root") {
		t.Fatalf("orphan selection error = %v", err)
	}
}

func TestContractBindingMakesProviderReachableFromDirectRoot(t *testing.T) {
	spec := ContractSpec{ID: "example.com/reachability/service", Major: 1}
	providerDefinition := testDefinition("example.com/reachability/provider")
	providerDefinition.Provides = []ContractSpec{spec}
	consumerDefinition := testDefinition("example.com/reachability/consumer")
	consumerDefinition.Consumes = []ContractRequirement{{ID: spec.ID, Major: spec.Major, Mode: ContractRequired}}
	provider := PluginProvider{Definition: providerDefinition, New: func() Plugin {
		return pluginFunc(func(registrar *Registrar) error { return ProvideContract(registrar, spec, "ready") })
	}}
	consumer := PluginProvider{Definition: consumerDefinition, New: func() Plugin {
		return pluginFunc(func(registrar *Registrar) error {
			_, err := RequireContract(registrar, spec, ContractRequired, (*string)(nil))
			return err
		})
	}}
	set := testSet(t, provider, consumer)
	for index := range set.Selections {
		set.Selections[index].Direct = set.Selections[index].ID == consumerDefinition.ID
	}
	if _, err := InspectPluginPlan(set); err != nil {
		t.Fatalf("contract-reachable provider rejected: %v", err)
	}
}
