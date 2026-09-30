// Package exampleplugin removes PluginSet boilerplate from runnable examples.
// Production hosts should load reviewed selections from their lockfile.
package exampleplugin

import (
	"encoding/json"
	"sort"

	wago "github.com/wago-org/wago"
)

// MustSet selects one provider and grants the exact scopes it requested.
func MustSet(definition wago.PluginDefinition, newPlugin func() wago.Plugin) wago.PluginSet {
	return MustSetProvider(wago.PluginProvider{
		Definition: definition,
		New:        newPlugin,
	}, nil)
}

// MustSetAll selects several providers and binds their declared Contracts.
func MustSetAll(providers ...wago.PluginProvider) wago.PluginSet {
	set := wago.PluginSet{Providers: providers}
	for _, provider := range providers {
		digest, err := wago.DefinitionDigest(provider.Definition)
		if err != nil {
			panic(err)
		}
		selection := wago.PluginSelection{
			ID:               provider.Definition.ID,
			DefinitionDigest: digest,
			Direct:           true,
			Dependencies:     map[string]string{},
		}
		for _, requirement := range provider.Definition.Requires {
			selection.Dependencies[requirement.ID] = requirement.Version
		}
		for _, request := range provider.Definition.Authorities {
			selection.Grants = append(selection.Grants, wago.AuthorityGrant{
				Name:  request.Name,
				Scope: request.Scope,
			})
		}
		for _, requirement := range provider.Definition.Consumes {
			var owners []string
			for _, candidate := range providers {
				for _, provided := range candidate.Definition.Provides {
					if provided.ID == requirement.ID && provided.Major == requirement.Major {
						owners = append(owners, candidate.Definition.ID)
					}
				}
			}
			sort.Strings(owners)
			selection.Contracts = append(selection.Contracts, wago.ContractBinding{
				ID:        requirement.ID,
				Major:     requirement.Major,
				Providers: owners,
			})
		}
		set.Selections = append(set.Selections, selection)
	}
	return set
}

// MustSetProvider selects one provider with the given reviewed configuration.
func MustSetProvider(provider wago.PluginProvider, config json.RawMessage) wago.PluginSet {
	digest, err := wago.DefinitionDigest(provider.Definition)
	if err != nil {
		panic(err)
	}

	grants := make([]wago.AuthorityGrant, len(provider.Definition.Authorities))
	for i, request := range provider.Definition.Authorities {
		grants[i] = wago.AuthorityGrant{
			Name:  request.Name,
			Scope: request.Scope,
		}
	}

	return wago.PluginSet{
		Providers: []wago.PluginProvider{provider},
		Selections: []wago.PluginSelection{{
			ID:               provider.Definition.ID,
			DefinitionDigest: digest,
			Direct:           true,
			Dependencies:     map[string]string{},
			Grants:           grants,
			Config:           config,
		}},
	}
}
