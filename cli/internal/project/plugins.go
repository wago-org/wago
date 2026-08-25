package project

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/wago-org/wago/src/core/semver"
)

type PluginRequirement struct {
	ID         string
	Constraint string
}

var (
	pluginIDPattern = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?\.)+[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:/[A-Za-z0-9](?:[A-Za-z0-9._~-]*[A-Za-z0-9])?)+$`)
)

func Requirements(dir string) ([]PluginRequirement, error) {
	manifest, err := Read(dir)
	if err != nil {
		return nil, err
	}
	return requirementsFromMap(manifest, dir)
}

func RequirementsFromManifest(manifest map[string]any) ([]PluginRequirement, error) {
	return requirementsFromMap(manifest, ".")
}

func SetRequirement(manifest map[string]any, id, constraint string) (bool, error) {
	if manifest == nil {
		return false, fmt.Errorf("manifest must be an object")
	}
	id, constraint = strings.TrimSpace(id), strings.TrimSpace(constraint)
	if err := ValidatePluginID(id); err != nil {
		return false, err
	}
	if err := ValidateConstraint(constraint); err != nil {
		return false, fmt.Errorf("plugin %q has invalid version constraint %q", id, constraint)
	}
	EnsureMetadata(manifest)
	raw, ok := manifest["plugins"]
	if !ok {
		raw = map[string]any{}
	}
	plugins, ok := raw.(map[string]any)
	if !ok {
		return false, fmt.Errorf("plugins must be an object mapping plugin IDs to version constraints")
	}
	if current, exists := plugins[id]; exists && current == constraint {
		return false, nil
	}
	plugins[id] = constraint
	manifest["plugins"] = plugins
	return true, nil
}

func DeleteRequirement(manifest map[string]any, id string) (bool, error) {
	if manifest == nil {
		return false, fmt.Errorf("manifest must be an object")
	}
	if err := ValidatePluginID(strings.TrimSpace(id)); err != nil {
		return false, err
	}
	raw, ok := manifest["plugins"]
	if !ok {
		return false, nil
	}
	plugins, ok := raw.(map[string]any)
	if !ok {
		return false, fmt.Errorf("plugins must be an object mapping plugin IDs to version constraints")
	}
	if _, ok := plugins[id]; !ok {
		return false, nil
	}
	delete(plugins, id)
	return true, nil
}

func Dependencies(dir string) ([]string, error) {
	requirements, err := Requirements(dir)
	if err != nil {
		return nil, err
	}
	dependencies := make([]string, len(requirements))
	for index := range requirements {
		dependencies[index] = requirements[index].ID
	}
	return dependencies, nil
}

func AddDependency(dir, id, constraint string) (bool, error) {
	id = strings.TrimSpace(id)
	if err := ValidatePluginID(id); err != nil {
		return false, err
	}
	constraint = strings.TrimSpace(constraint)
	if err := ValidateConstraint(constraint); err != nil {
		return false, fmt.Errorf("plugin %q has invalid version constraint %q", id, constraint)
	}
	var changed bool
	err := WithMutation(context.Background(), dir, func(mutation *Mutation) error {
		manifest, err := mutation.ReadManifest()
		if err != nil {
			return err
		}
		changed, err = SetRequirement(manifest, id, constraint)
		if err != nil || !changed {
			return err
		}
		return mutation.PublishManifest(manifest)
	})
	return changed, err
}

func RemoveDependency(dir, name string) (removed bool, module string, err error) {
	id := strings.TrimSpace(name)
	err = WithMutation(context.Background(), dir, func(mutation *Mutation) error {
		manifest, readErr := mutation.ReadManifest()
		if readErr != nil {
			return readErr
		}
		requirements, readErr := requirementsFromMap(manifest, dir)
		if readErr != nil {
			return readErr
		}
		for _, requirement := range requirements {
			if requirement.ID == id {
				module = requirement.ID
				break
			}
		}
		if module == "" {
			return nil
		}
		_, lockStatErr := os.Stat(LockPath(dir))
		if lockStatErr != nil && !os.IsNotExist(lockStatErr) {
			return lockStatErr
		}
		if os.IsNotExist(lockStatErr) {
			delete(manifest["plugins"].(map[string]any), id)
			if publishErr := mutation.PublishManifest(manifest); publishErr != nil {
				return publishErr
			}
			removed = true
			return nil
		}
		lock, readErr := mutation.ReadLock()
		if readErr != nil {
			return readErr
		}
		delete(manifest["plugins"].(map[string]any), id)
		if entry, ok := lock.Plugins[id]; ok {
			entry.Direct = false
			lock.Plugins[id] = entry
		}
		pruneLock(&lock)
		if publishErr := mutation.PublishMetadata(manifest, lock); publishErr != nil {
			return publishErr
		}
		removed = true
		return nil
	})
	if err != nil {
		return false, "", err
	}
	return removed, module, nil
}

func pruneLock(document *LockDocument) {
	reachable := make(map[string]bool, len(document.Plugins))
	var visit func(string)
	visit = func(id string) {
		if reachable[id] {
			return
		}
		entry, ok := document.Plugins[id]
		if !ok {
			return
		}
		reachable[id] = true
		for dependency := range entry.Dependencies {
			visit(dependency)
		}
		for _, binding := range entry.Bindings {
			for _, provider := range binding.Providers {
				visit(provider)
			}
		}
	}
	for id, entry := range document.Plugins {
		if entry.Direct {
			visit(id)
		}
	}
	for id := range document.Plugins {
		if !reachable[id] {
			delete(document.Plugins, id)
		}
	}
}

func requirementsFromMap(manifest map[string]any, dir string) ([]PluginRequirement, error) {
	if _, legacy := manifest["schema"]; legacy {
		return nil, fmt.Errorf("%s: schema was removed; use $schema with %q for editor tooling", DisplayPath(dir), SchemaURI)
	}
	if _, legacy := manifest["dependencies"]; legacy {
		return nil, fmt.Errorf("%s: dependencies was removed; declare versioned entries under plugins", DisplayPath(dir))
	}
	if schema, ok := manifest["$schema"]; ok && schema != SchemaURI {
		return nil, fmt.Errorf("%s: unsupported $schema %q; plugin projects require %q", DisplayPath(dir), schema, SchemaURI)
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
		if err := ValidatePluginID(id); err != nil {
			return nil, fmt.Errorf("%s: %w", DisplayPath(dir), err)
		}
		constraint, ok := rawConstraint.(string)
		if !ok || ValidateConstraint(strings.TrimSpace(constraint)) != nil {
			return nil, fmt.Errorf("%s: plugin %q has invalid version constraint %q", DisplayPath(dir), id, rawConstraint)
		}
		requirements = append(requirements, PluginRequirement{
			ID:         id,
			Constraint: strings.TrimSpace(constraint),
		})
	}
	sort.Slice(requirements, func(i, j int) bool { return requirements[i].ID < requirements[j].ID })
	return requirements, nil
}

func ValidateConstraint(constraint string) error {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return fmt.Errorf("version constraint is empty")
	}
	if len(constraint) > 200 {
		return fmt.Errorf("version constraint is longer than 200 characters")
	}
	if _, err := semver.ParseConstraint(constraint); err != nil {
		return err
	}
	return nil
}

// ValidatePluginID accepts only canonical Go module or package paths. Plugin
// IDs are never registry-relative aliases: the same full path identifies a
// provider in the manifest, catalog, lockfile, generated build, and runtime.
func ValidatePluginID(id string) error {
	if len(id) > 300 || !pluginIDPattern.MatchString(id) {
		return fmt.Errorf("plugin ID %q must be fully qualified, such as github.com/wago-org/wasi", id)
	}
	return nil
}

// ExpandGitHubPluginID expands an owner/repository[/subpackage] CLI shorthand
// to its canonical GitHub import path. Invalid and already-qualified IDs are
// left unchanged so the normal validation error remains actionable.
func ExpandGitHubPluginID(id string) string {
	id = strings.TrimSpace(id)
	if ValidatePluginID(id) == nil || !strings.Contains(id, "/") {
		return id
	}
	host, _, _ := strings.Cut(id, "/")
	if strings.Contains(host, ".") {
		return id
	}
	candidate := "github.com/" + id
	if ValidatePluginID(candidate) != nil {
		return id
	}
	return candidate
}
