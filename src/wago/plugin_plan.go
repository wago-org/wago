package wago

import (
	"fmt"
	"net/url"
	"sort"
)

type plannedExtension struct {
	name string
	ext  Extension
	info ExtensionInfo
	reg  *Registry
}

// LoadPlugins resolves, authorizes, and transactionally registers a complete
// plugin plan. It is intended for wago.json-driven startup and must run before
// any individual Use calls. Required dependencies load first; before/after
// constraints add ordering edges; ties are resolved lexically by registry name.
func (rt *Runtime) LoadPlugins(configs []PluginConfig) error {
	ordered, err := resolvePluginOrder(configs)
	if err != nil {
		return err
	}
	planned := make([]plannedExtension, 0, len(ordered))
	for _, cfg := range ordered {
		ext, ok := NewExtension(cfg.Name)
		if !ok {
			return fmt.Errorf("wago: plugin %q is not compiled into this binary", cfg.Name)
		}
		info := ext.Info()
		if info.ID == "" {
			return fmt.Errorf("wago: plugin %q has no extension ID", cfg.Name)
		}
		if err := validateOpenSourcePlugin(info); err != nil {
			return &ExtensionError{Extension: info.ID, Operation: "provenance", Err: err}
		}
		if err := checkCompat(info.Compat); err != nil {
			return &ExtensionError{Extension: info.ID, Operation: "load", Err: err}
		}
		grants := make(map[PluginCapability]struct{}, len(cfg.Capabilities))
		for _, cap := range cfg.Capabilities {
			if !validPluginCapability(cap) {
				return fmt.Errorf("wago: plugin %q requests unknown plugin capability %q", cfg.Name, cap)
			}
			grants[cap] = struct{}{}
		}
		for _, cap := range info.RequiresCapabilities {
			if _, ok := grants[cap]; !ok {
				return &ExtensionError{Extension: info.ID, Operation: "authorize", Err: fmt.Errorf("plugin capability %q was not granted: %w", cap, ErrPermissionDenied)}
			}
		}
		reg := &Registry{info: info, hooks: &HookRegistry{}, grants: grants, config: cfg.Config}
		if err := ext.Register(reg); err != nil {
			return &ExtensionError{Extension: info.ID, Operation: "register", Err: err}
		}
		for _, cap := range reg.requiredPluginCapabilities() {
			if _, ok := grants[cap]; !ok {
				return &ExtensionError{Extension: info.ID, Operation: "authorize", Err: fmt.Errorf("plugin exercised capability %q without a grant: %w", cap, ErrPermissionDenied)}
			}
		}
		planned = append(planned, plannedExtension{name: cfg.Name, ext: ext, info: info, reg: reg})
	}
	return rt.commitPluginPlan(planned)
}

func validateOpenSourcePlugin(info ExtensionInfo) error {
	if info.Private {
		return fmt.Errorf("manifest-loaded plugins must be open source; private plugins are not allowed")
	}
	if info.Repository == "" {
		return fmt.Errorf("manifest-loaded plugin must declare its source repository")
	}
	u, err := url.Parse(info.Repository)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("plugin repository %q must be an absolute https URL", info.Repository)
	}
	if info.License == "" {
		return fmt.Errorf("manifest-loaded plugin must declare an SPDX license identifier")
	}
	return nil
}

func resolvePluginOrder(configs []PluginConfig) ([]PluginConfig, error) {
	byName := make(map[string]PluginConfig, len(configs))
	infos := make(map[string]ExtensionInfo, len(configs))
	for _, cfg := range configs {
		if cfg.Name == "" {
			return nil, fmt.Errorf("wago: plugin plan contains an empty name")
		}
		if _, exists := byName[cfg.Name]; exists {
			return nil, fmt.Errorf("wago: plugin %q appears more than once", cfg.Name)
		}
		ext, ok := NewExtension(cfg.Name)
		if !ok {
			return nil, fmt.Errorf("wago: plugin %q is not compiled into this binary", cfg.Name)
		}
		byName[cfg.Name], infos[cfg.Name] = cfg, ext.Info()
	}
	edges := make(map[string]map[string]struct{}, len(configs))
	indegree := make(map[string]int, len(configs))
	for name := range byName {
		edges[name] = map[string]struct{}{}
	}
	addEdge := func(from, to string) {
		if from == to {
			return
		}
		if _, exists := edges[from][to]; !exists {
			edges[from][to] = struct{}{}
			indegree[to]++
		}
	}
	for name, cfg := range byName {
		info := infos[name]
		for _, dep := range info.Requires {
			if _, ok := byName[dep]; !ok {
				return nil, fmt.Errorf("wago: plugin %q requires missing plugin %q", name, dep)
			}
			addEdge(dep, name)
		}
		for _, after := range append(append([]string(nil), info.After...), cfg.After...) {
			if _, ok := byName[after]; ok {
				addEdge(after, name)
			}
		}
		for _, before := range append(append([]string(nil), info.Before...), cfg.Before...) {
			if _, ok := byName[before]; ok {
				addEdge(name, before)
			}
		}
	}
	ready := make([]string, 0, len(configs))
	for name := range byName {
		if indegree[name] == 0 {
			ready = append(ready, name)
		}
	}
	sort.Strings(ready)
	result := make([]PluginConfig, 0, len(configs))
	for len(ready) != 0 {
		name := ready[0]
		ready = ready[1:]
		result = append(result, byName[name])
		for next := range edges[name] {
			indegree[next]--
			if indegree[next] == 0 {
				ready = append(ready, next)
				sort.Strings(ready)
			}
		}
	}
	if len(result) != len(configs) {
		var cycle []string
		for name := range byName {
			if indegree[name] != 0 {
				cycle = append(cycle, name)
			}
		}
		sort.Strings(cycle)
		return nil, fmt.Errorf("wago: plugin load-order cycle among %v", cycle)
	}
	return result, nil
}

func (rt *Runtime) commitPluginPlan(plan []plannedExtension) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return fmt.Errorf("wago: LoadPlugins on a closed runtime")
	}
	if len(rt.exts) != 0 {
		return fmt.Errorf("wago: LoadPlugins must run before individual plugin registration")
	}
	moduleOwner := map[string]string{}
	importOwner := map[string]string{}
	ids := map[string]struct{}{}
	for _, p := range plan {
		if _, duplicate := ids[p.info.ID]; duplicate {
			return &ExtensionError{Extension: p.info.ID, Operation: "load", Err: ErrExtensionConflict}
		}
		ids[p.info.ID] = struct{}{}
		for _, imp := range p.reg.imports {
			if imp.fn == nil {
				return &ExtensionError{Extension: p.info.ID, Operation: "register", Err: fmt.Errorf("import %q has no function", imp.key())}
			}
			if owner, ok := moduleOwner[imp.module]; ok && owner != p.info.ID && rt.overridePolicy != AllowTestOverrides {
				return &ExtensionError{Extension: p.info.ID, Operation: "register", Err: fmt.Errorf("import module %q already owned by extension %q: %w", imp.module, owner, ErrExtensionConflict)}
			}
			if owner, ok := importOwner[imp.key()]; ok && owner != p.info.ID && rt.overridePolicy != AllowTestOverrides {
				return &ExtensionError{Extension: p.info.ID, Operation: "register", Err: fmt.Errorf("import %q already provided by extension %q: %w", imp.key(), owner, ErrExtensionConflict)}
			}
			moduleOwner[imp.module], importOwner[imp.key()] = p.info.ID, p.info.ID
		}
	}
	for _, p := range plan {
		for _, imp := range p.reg.imports {
			rt.imports[imp.key()] = imp.fn
			rt.importMeta[imp.key()] = imp
			rt.importOwner[imp.key()] = p.info.ID
			rt.moduleOwner[imp.module] = p.info.ID
		}
		for _, spec := range p.reg.caps {
			if _, ok := rt.caps[spec.cap]; !ok {
				rt.capOrder = append(rt.capOrder, spec.cap)
			}
			rt.caps[spec.cap] = p.info.ID
		}
		rt.hooks.appendFrom(p.reg.hooks)
		for _, manager := range p.reg.managers {
			manager.activate(rt)
		}
		for _, activate := range p.reg.activate {
			activate(rt)
		}
		rt.exts = append(rt.exts, p.info)
		rt.extensions[p.info.ID] = p.ext
	}
	return nil
}
