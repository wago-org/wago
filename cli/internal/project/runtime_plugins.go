//go:build !wago_minimal

package project

import (
	"encoding/json"
	"fmt"
	"sort"
)

// PluginSelection is the authority-bearing handoff from a strict lockfile to
// the explicitly linked provider catalog. It intentionally has no ordering
// overrides; dependencies and Contracts determine the complete plan.
type PluginSelection struct {
	ID               string            `json:"id"`
	DefinitionDigest string            `json:"definitionDigest"`
	Direct           bool              `json:"direct"`
	Dependencies     map[string]string `json:"dependencies"`
	Grants           []AuthorityGrant  `json:"grants"`
	Contracts        []ContractBinding `json:"contracts"`
	Config           json.RawMessage   `json:"config"`
}

func PluginSelections(dir string) ([]PluginSelection, error) {
	lock, err := ReadLock(dir)
	if err != nil {
		return nil, err
	}
	requirements, err := Requirements(dir)
	if err != nil {
		return nil, err
	}
	if err := ValidateLockedResolution(requirements, lock); err != nil {
		return nil, fmt.Errorf("%s: %w", DisplayPath(dir), err)
	}
	selections := make([]PluginSelection, 0, len(lock.Plugins))
	ids := sortedLockKeys(lock.Plugins)
	for _, id := range ids {
		entry := lock.Plugins[id]
		selections = append(selections, PluginSelection{
			ID: id, DefinitionDigest: entry.DefinitionDigest,
			Direct: entry.Direct, Dependencies: cloneDependencyConstraints(entry.Dependencies),
			Grants:    append([]AuthorityGrant(nil), entry.Grants...),
			Contracts: append([]ContractBinding(nil), entry.Bindings...),
			Config:    append(json.RawMessage(nil), entry.Config...),
		})
	}
	sort.Slice(selections, func(i, j int) bool { return selections[i].ID < selections[j].ID })
	return selections, nil
}

func cloneDependencyConstraints(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for id, constraint := range input {
		output[id] = constraint
	}
	return output
}
