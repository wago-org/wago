package wago

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
)

type plannedExtension struct {
	name string
	cfg  PluginConfig
	ext  Extension
	info ExtensionInfo
	reg  *Registry
}

// LoadPlugins resolves, authorizes, and transactionally registers a complete
// plugin plan. It is intended for wago.json-driven startup and must run before
// any individual Use calls. Required dependencies load first; before/after
// constraints add ordering edges; ties are resolved lexically by registry name.
func (rt *Runtime) LoadPlugins(configs []PluginConfig) error {
	planned, err := planPlugins(configs)
	if err != nil {
		return err
	}
	if err := rt.commitPluginPlan(planned); err != nil {
		return err
	}
	return rt.startPluginPlan(context.Background(), planned)
}

func planPlugins(configs []PluginConfig) ([]plannedExtension, error) {
	ordered, err := resolvePluginOrder(configs)
	if err != nil {
		return nil, err
	}
	planned := make([]plannedExtension, 0, len(ordered))
	for _, cfg := range ordered {
		ext, ok := NewExtension(cfg.Name)
		if !ok {
			return nil, fmt.Errorf("wago: plugin %q is not compiled into this binary", cfg.Name)
		}
		info := ext.Info()
		if info.ID == "" {
			return nil, fmt.Errorf("wago: plugin %q has no extension ID", cfg.Name)
		}
		if err := validateOpenSourcePlugin(info); err != nil {
			return nil, &ExtensionError{Extension: info.ID, Operation: "provenance", Err: err}
		}
		if err := checkCompat(info.Compat); err != nil {
			return nil, &ExtensionError{Extension: info.ID, Operation: "load", Err: err}
		}
		if provider, ok := ext.(ConfigSchemaProvider); ok {
			schema := provider.ConfigSchema()
			if len(schema) != 0 && !json.Valid(schema) {
				return nil, &PluginError{Plugin: cfg.Name, Phase: PluginPhaseConfigure, Path: "configSchema", Err: fmt.Errorf("invalid JSON")}
			}
		}
		grants := make(map[PluginCapability]struct{}, len(cfg.Capabilities))
		for _, cap := range cfg.Capabilities {
			if !validPluginCapability(cap) {
				return nil, fmt.Errorf("wago: plugin %q requests unknown plugin capability %q", cfg.Name, cap)
			}
			grants[cap] = struct{}{}
		}
		for _, cap := range info.RequiresCapabilities {
			if _, ok := grants[cap]; !ok {
				return nil, &ExtensionError{Extension: info.ID, Operation: "authorize", Err: fmt.Errorf("plugin capability %q was not granted: %w", cap, ErrPermissionDenied)}
			}
		}
		reg := &Registry{info: info, hooks: &HookRegistry{}, grants: grants, budgets: cfg.Budgets, config: cfg.Config}
		if err := ext.Register(reg); err != nil {
			return nil, &ExtensionError{Extension: info.ID, Operation: "register", Err: err}
		}
		for _, cap := range reg.requiredPluginCapabilities() {
			if _, ok := grants[cap]; !ok {
				return nil, &ExtensionError{Extension: info.ID, Operation: "authorize", Err: fmt.Errorf("plugin exercised capability %q without a grant: %w", cap, ErrPermissionDenied)}
			}
		}
		planned = append(planned, plannedExtension{name: cfg.Name, cfg: cfg, ext: ext, info: info, reg: reg})
	}
	planned, err = resolveServiceOrder(planned)
	if err != nil {
		return nil, err
	}
	return planned, nil
}

// PluginPlan is a dry-run report of the fully resolved plugin graph.
type PluginPlan struct {
	Plugins []PluginPlanEntry `json:"plugins"`
}

type PluginPlanEntry struct {
	Name         string                                `json:"name"`
	ID           string                                `json:"id"`
	Capabilities []PluginCapability                    `json:"capabilities,omitempty"`
	Budgets      map[PluginCapability]CapabilityBudget `json:"budgets,omitempty"`
	Provides     []string                              `json:"provides,omitempty"`
	Requires     []string                              `json:"requires,omitempty"`
	ConfigSchema json.RawMessage                       `json:"configSchema,omitempty"`
}

// InspectPluginPlan validates and resolves configs without committing or
// starting plugins. Register must therefore remain declarative.
func InspectPluginPlan(configs []PluginConfig) (*PluginPlan, error) {
	planned, err := planPlugins(configs)
	if err != nil {
		return nil, err
	}
	report := &PluginPlan{Plugins: make([]PluginPlanEntry, 0, len(planned))}
	for _, p := range planned {
		entry := PluginPlanEntry{Name: p.name, ID: p.info.ID, Capabilities: append([]PluginCapability(nil), p.cfg.Capabilities...), Budgets: p.cfg.Budgets}
		for _, service := range p.reg.provides {
			entry.Provides = append(entry.Provides, service.name)
		}
		for _, service := range p.reg.requires {
			entry.Requires = append(entry.Requires, service.serviceName())
		}
		if provider, ok := p.ext.(ConfigSchemaProvider); ok {
			entry.ConfigSchema = provider.ConfigSchema()
		}
		sort.Slice(entry.Capabilities, func(i, j int) bool { return entry.Capabilities[i] < entry.Capabilities[j] })
		sort.Strings(entry.Provides)
		sort.Strings(entry.Requires)
		report.Plugins = append(report.Plugins, entry)
	}
	return report, nil
}

func (rt *Runtime) startPluginPlan(ctx context.Context, plan []plannedExtension) error {
	for _, p := range plan {
		if starter, ok := p.ext.(PluginStarter); ok {
			if err := starter.Start(ctx, &PluginHost{Runtime: rt, Plugin: p.name}); err != nil {
				startErr := &PluginError{Plugin: p.name, Phase: PluginPhaseStart, Err: err}
				var failedStop error
				if stopper, ok := p.ext.(PluginStopper); ok {
					if err := stopper.Stop(ctx); err != nil {
						failedStop = &PluginError{Plugin: p.name, Phase: PluginPhaseStop, Err: err}
					}
				}
				return errors.Join(startErr, failedStop, rt.CloseContext(ctx))
			}
		}
		if stopper, ok := p.ext.(PluginStopper); ok {
			rt.mu.Lock()
			rt.pluginStops = append(rt.pluginStops, registeredPluginStop{name: p.name, stop: stopper.Stop})
			rt.mu.Unlock()
		}
	}
	return nil
}

func resolveServiceOrder(plan []plannedExtension) ([]plannedExtension, error) {
	byName := make(map[string]plannedExtension, len(plan))
	edges := make(map[string]map[string]struct{}, len(plan))
	indegree := make(map[string]int, len(plan))
	providers := map[string]struct {
		plugin string
		item   serviceProvision
	}{}
	for _, p := range plan {
		byName[p.name] = p
		edges[p.name] = map[string]struct{}{}
		for _, provided := range p.reg.provides {
			if previous, duplicate := providers[provided.name]; duplicate {
				return nil, &PluginError{Plugin: p.name, Phase: PluginPhaseResolve, Err: fmt.Errorf("service %q is already provided by plugin %q", provided.name, previous.plugin)}
			}
			providers[provided.name] = struct {
				plugin string
				item   serviceProvision
			}{p.name, provided}
		}
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
	for _, p := range plan {
		for _, dep := range p.info.Requires {
			addEdge(dep, p.name)
		}
		for _, after := range append(append([]string(nil), p.info.After...), p.cfg.After...) {
			if _, ok := byName[after]; ok {
				addEdge(after, p.name)
			}
		}
		for _, before := range append(append([]string(nil), p.info.Before...), p.cfg.Before...) {
			if _, ok := byName[before]; ok {
				addEdge(p.name, before)
			}
		}
		for _, required := range p.reg.requires {
			provider, ok := providers[required.serviceName()]
			if !ok {
				return nil, &PluginError{Plugin: p.name, Phase: PluginPhaseResolve, Err: fmt.Errorf("required service %q has no provider", required.serviceName())}
			}
			if required.serviceType() != nil && provider.item.typ != required.serviceType() {
				return nil, &PluginError{Plugin: p.name, Phase: PluginPhaseResolve, Err: fmt.Errorf("service %q type mismatch: provider has %v, consumer wants %v", required.serviceName(), provider.item.typ, required.serviceType())}
			}
			addEdge(provider.plugin, p.name)
		}
	}
	ready := make([]string, 0, len(plan))
	for name := range byName {
		if indegree[name] == 0 {
			ready = append(ready, name)
		}
	}
	sort.Strings(ready)
	ordered := make([]plannedExtension, 0, len(plan))
	for len(ready) != 0 {
		name := ready[0]
		ready = ready[1:]
		ordered = append(ordered, byName[name])
		for next := range edges[name] {
			indegree[next]--
			if indegree[next] == 0 {
				ready = append(ready, next)
				sort.Strings(ready)
			}
		}
	}
	if len(ordered) != len(plan) {
		return nil, fmt.Errorf("wago: plugin service/load-order cycle")
	}
	for _, p := range ordered {
		for _, required := range p.reg.requires {
			if err := required.bindService(providers[required.serviceName()].item.value); err != nil {
				return nil, &PluginError{Plugin: p.name, Phase: PluginPhaseResolve, Err: err}
			}
		}
	}
	return ordered, nil
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
