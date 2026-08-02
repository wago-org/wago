//go:build !wago_minimal

package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type PluginIntent struct {
	Name         string
	Capabilities []string
	Budgets      map[string]CapabilityBudget
	Before       []string
	After        []string
	Config       json.RawMessage
}

type CapabilityBudget struct {
	MaxInstances   uint32 `json:"maxInstances,omitempty"`
	MaxMemoryBytes uint64 `json:"maxMemoryBytes,omitempty"`
}

// PluginIntents combines wago.json's selected plugins with the exact authority
// and opaque configuration resolved in wago-lock.json.
func PluginIntents(dir string) ([]PluginIntent, error) {
	requirements, err := Requirements(dir)
	if err != nil {
		return nil, err
	}
	lock, err := ReadLock(dir)
	if err != nil {
		return nil, err
	}
	intents := make([]PluginIntent, 0, len(requirements))
	for _, requirement := range requirements {
		entry := lock.Packages[requirement.ID]
		capabilities, budgets, err := parseCapabilities(requirement.ID, entry.Capabilities)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", DisplayPath(dir), err)
		}
		intents = append(intents, PluginIntent{
			Name:         requirement.ID,
			Capabilities: capabilities,
			Budgets:      budgets,
			Config:       append(json.RawMessage(nil), entry.Config...),
		})
	}
	return intents, nil
}

func parseCapabilities(name string, raw json.RawMessage) ([]string, map[string]CapabilityBudget, error) {
	if len(raw) == 0 {
		raw = []byte("[]")
	}
	var values []string
	budgets := map[string]CapabilityBudget{}
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, nil, fmt.Errorf("plugin %q capabilities: %w", name, err)
		}
	} else {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, nil, fmt.Errorf("plugin %q capabilities: %w", name, err)
		}
		for value, options := range object {
			values = append(values, value)
			if bytes.Equal(options, []byte("true")) {
				continue
			}
			var budget CapabilityBudget
			if err := json.Unmarshal(options, &budget); err != nil {
				return nil, nil, fmt.Errorf("plugin %q capability %q: %w", name, value, err)
			}
			budgets[value] = budget
		}
		sort.Strings(values)
	}
	capabilities := make([]string, len(values))
	seen := map[string]struct{}{}
	for i, value := range values {
		capability := strings.TrimSpace(value)
		if capability == "" {
			return nil, nil, fmt.Errorf("plugin %q has an empty capability", name)
		}
		if _, duplicate := seen[capability]; duplicate {
			return nil, nil, fmt.Errorf("plugin %q repeats capability %q", name, capability)
		}
		seen[capability], capabilities[i] = struct{}{}, capability
	}
	if len(budgets) == 0 {
		budgets = nil
	}
	return capabilities, budgets, nil
}
