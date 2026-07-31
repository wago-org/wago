package project

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type PluginRequirement struct {
	ID         string
	Module     string
	Constraint string
}

var (
	pluginIDPattern         = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._/-]*$`)
	pluginConstraintPattern = regexp.MustCompile(`^(?:\*|[~^]?v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)$`)
)

func Requirements(dir string) ([]PluginRequirement, error) {
	manifest, err := Read(dir)
	if err != nil {
		return nil, err
	}
	return requirementsFromMap(manifest, dir)
}

func Dependencies(dir string) ([]string, error) {
	requirements, err := Requirements(dir)
	if err != nil {
		return nil, err
	}
	dependencies := make([]string, len(requirements))
	for index := range requirements {
		dependencies[index] = requirements[index].Module
	}
	return dependencies, nil
}

func AddDependency(dir, module, constraint string) (bool, error) {
	if !strings.HasPrefix(module, "github.com/") {
		return false, fmt.Errorf("plugin module %q must be hosted on github.com", module)
	}
	constraint = strings.TrimSpace(constraint)
	if !pluginConstraintPattern.MatchString(constraint) {
		return false, fmt.Errorf("plugin %q has invalid version constraint %q", strings.TrimPrefix(module, "github.com/"), constraint)
	}
	manifest, err := Read(dir)
	if err != nil {
		return false, err
	}
	EnsureMetadata(manifest)
	raw, ok := manifest["plugins"]
	if !ok {
		raw = map[string]any{}
	}
	plugins, ok := raw.(map[string]any)
	if !ok {
		return false, fmt.Errorf("%s: plugins must be an object mapping plugin IDs to version constraints", DisplayPath(dir))
	}
	id := strings.TrimPrefix(module, "github.com/")
	if current, exists := plugins[id]; exists && current == constraint {
		return false, nil
	}
	plugins[id] = constraint
	manifest["plugins"] = plugins
	return true, Write(dir, manifest)
}

func RemoveDependency(dir, name string) (removed bool, module string, err error) {
	manifest, err := Read(dir)
	if err != nil {
		return false, "", err
	}
	requirements, err := requirementsFromMap(manifest, dir)
	if err != nil {
		return false, "", err
	}
	id := strings.TrimPrefix(name, "github.com/")
	var matchedModule string
	for _, requirement := range requirements {
		if requirement.ID == id || requirement.Module == name {
			id, matchedModule = requirement.ID, requirement.Module
			break
		}
	}
	if matchedModule == "" {
		return false, "", nil
	}
	delete(manifest["plugins"].(map[string]any), id)
	if err := Write(dir, manifest); err != nil {
		return false, "", err
	}
	lock, err := ReadLock(dir)
	if err != nil {
		return false, "", err
	}
	delete(lock.Packages, id)
	if err := WriteLock(dir, lock); err != nil {
		return false, "", err
	}
	return true, matchedModule, nil
}

func requirementsFromMap(manifest map[string]any, dir string) ([]PluginRequirement, error) {
	if _, legacy := manifest["schema"]; legacy {
		return nil, fmt.Errorf("%s: schema was removed; use $schema with %q for editor tooling", DisplayPath(dir), SchemaURI)
	}
	if _, legacy := manifest["dependencies"]; legacy {
		return nil, fmt.Errorf("%s: dependencies was removed; declare versioned entries under plugins", DisplayPath(dir))
	}
	raw, ok := manifest["plugins"]
	if !ok {
		return nil, nil
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: plugins must be an object mapping plugin IDs to version constraints", DisplayPath(dir))
	}
	requirements := make([]PluginRequirement, 0, len(values))
	for id, rawConstraint := range values {
		id = strings.TrimSpace(id)
		if !pluginIDPattern.MatchString(id) {
			return nil, fmt.Errorf("%s: plugin ID %q must be GitHub-relative, such as wago-org/wasi", DisplayPath(dir), id)
		}
		constraint, ok := rawConstraint.(string)
		if !ok || !pluginConstraintPattern.MatchString(strings.TrimSpace(constraint)) {
			return nil, fmt.Errorf("%s: plugin %q has invalid version constraint %q", DisplayPath(dir), id, rawConstraint)
		}
		requirements = append(requirements, PluginRequirement{
			ID:         id,
			Module:     "github.com/" + id,
			Constraint: strings.TrimSpace(constraint),
		})
	}
	sort.Slice(requirements, func(i, j int) bool { return requirements[i].ID < requirements[j].ID })
	return requirements, nil
}
