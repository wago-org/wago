package plugin

import (
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"
)

func scopedGrantLock() project.LockDocument {
	lock := project.NewLockDocument()
	lock.Plugins["github.com/acme/plugin"] = project.LockEntry{
		Direct:             true,
		Source:             project.PluginSource{Module: "github.com/acme/plugin", Version: "v1.0.0", Checksum: "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
		Provider:           project.ProviderSource{ImportPath: "github.com/acme/plugin/register"},
		DefinitionDigest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ReleaseFingerprint: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Dependencies:       map[string]string{},
		RequestedAuthorities: []project.AuthorityRequest{
			{Name: "host.import.define", Mode: project.AuthorityRequired, Reason: "imports", Scope: project.AuthorityScope{Modules: []string{"clock", "random"}}},
			{Name: "instance.manage", Mode: project.AuthorityOptional, Reason: "workers", Scope: project.AuthorityScope{MaxInstances: 8, MaxMemoryBytes: 1 << 20}},
			{Name: "runtime.close.observe", Mode: project.AuthorityRequired, Reason: "shutdown"},
		},
		Grants: []project.AuthorityGrant{
			{Name: "host.import.define", Scope: project.AuthorityScope{Modules: []string{"clock", "random"}}},
			{Name: "instance.manage", Scope: project.AuthorityScope{MaxInstances: 8, MaxMemoryBytes: 1 << 20}},
			{Name: "runtime.close.observe"},
		},
		Contracts: project.ContractSet{Provides: []project.ContractProvider{}, Requires: []project.ContractRequirement{}},
		Bindings:  []project.ContractBinding{},
		Config:    []byte(`{}`),
	}
	return lock
}

func TestEditAuthorityGrantsNarrowsNamedAndResourceScopes(t *testing.T) {
	lock := scopedGrantLock()
	scopes := map[string]map[string]project.AuthorityScope{
		"github.com/acme/plugin": {
			"host.import.define": {Modules: []string{"clock"}},
			"instance.manage":    {MaxInstances: 2, MaxMemoryBytes: 65536},
		},
	}
	got, err := editAuthorityGrants(lock, "github.com/acme/plugin", nil, false, false, scopes)
	if err != nil {
		t.Fatal(err)
	}
	want := []project.AuthorityGrant{
		{Name: "host.import.define", Scope: project.AuthorityScope{Modules: []string{"clock"}}},
		{Name: "instance.manage", Scope: project.AuthorityScope{MaxInstances: 2, MaxMemoryBytes: 65536}},
		{Name: "runtime.close.observe"},
	}
	if !reflect.DeepEqual(got.Plugins["github.com/acme/plugin"].Grants, want) {
		t.Fatalf("grants = %#v, want %#v", got.Plugins["github.com/acme/plugin"].Grants, want)
	}
}

func TestEditAuthorityGrantsRejectsWrongPluginUnknownDeniedAndWidenedScopes(t *testing.T) {
	tests := []struct {
		name      string
		requested []string
		denyAll   bool
		scopes    map[string]map[string]project.AuthorityScope
		want      string
	}{
		{name: "wrong plugin", scopes: map[string]map[string]project.AuthorityScope{"github.com/acme/other": {"host.import.define": {Modules: []string{"clock"}}}}, want: "targets plugin"},
		{name: "unknown authority", scopes: map[string]map[string]project.AuthorityScope{"github.com/acme/plugin": {"module.close.observe": {}}}, want: "does not request authority"},
		{name: "denied optional", requested: []string{}, scopes: map[string]map[string]project.AuthorityScope{"github.com/acme/plugin": {"instance.manage": {MaxInstances: 2, MaxMemoryBytes: 65536}}}, want: "is not granted"},
		{name: "module widened", scopes: map[string]map[string]project.AuthorityScope{"github.com/acme/plugin": {"host.import.define": {Modules: []string{"clock", "network"}}}}, want: "outside the requested scope"},
		{name: "instances widened", scopes: map[string]map[string]project.AuthorityScope{"github.com/acme/plugin": {"instance.manage": {MaxInstances: 9, MaxMemoryBytes: 65536}}}, want: "outside the requested scope"},
		{name: "memory widened", scopes: map[string]map[string]project.AuthorityScope{"github.com/acme/plugin": {"instance.manage": {MaxInstances: 2, MaxMemoryBytes: 2 << 20}}}, want: "outside the requested scope"},
		{name: "zero limit", scopes: map[string]map[string]project.AuthorityScope{"github.com/acme/plugin": {"instance.manage": {MaxInstances: 0, MaxMemoryBytes: 65536}}}, want: "positive"},
		{name: "duplicate module", scopes: map[string]map[string]project.AuthorityScope{"github.com/acme/plugin": {"host.import.define": {Modules: []string{"clock", "clock"}}}}, want: "duplicate"},
		{name: "unscoped authority", scopes: map[string]map[string]project.AuthorityScope{"github.com/acme/plugin": {"runtime.close.observe": {}}}, want: "does not have a configurable scope"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := editAuthorityGrants(scopedGrantLock(), "github.com/acme/plugin", test.requested, false, test.denyAll, test.scopes)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNonInteractiveResolutionSeparatesScopeAndOptionalSelection(t *testing.T) {
	automation.Configure(automation.Options{NoInput: true})
	t.Cleanup(automation.Reset)
	lock := scopedGrantLock()
	entry := lock.Plugins["github.com/acme/plugin"]
	required := entry.RequestedAuthorities[0]
	optional := entry.RequestedAuthorities[1]
	plan := ResolutionPlan{Lock: lock, Reviews: []AuthorityReview{
		{PluginID: "github.com/acme/plugin", Request: required, Proposed: project.AuthorityGrant{Name: required.Name, Scope: required.Scope}, Change: "changed"},
		{PluginID: "github.com/acme/plugin", Request: optional, Proposed: project.AuthorityGrant{Name: optional.Name, Scope: optional.Scope}, Change: "changed"},
	}}
	scopes := map[string]map[string]project.AuthorityScope{
		"github.com/acme/plugin": {"host.import.define": {Modules: []string{"clock"}}},
	}
	if _, err := reviewResolvedPluginPlan(plan, pkgOpts{scopes: scopes}); err == nil || !strings.Contains(err.Error(), "authority review") {
		t.Fatalf("scope-only authority review error = %v", err)
	}
	reviewed, err := reviewResolvedPluginPlan(plan, pkgOpts{denyAll: true})
	if err != nil {
		t.Fatal(err)
	}
	got := reviewed.Lock
	if grants := got.Plugins["github.com/acme/plugin"].Grants; len(grants) != 1 || grants[0].Name != "runtime.close.observe" {
		t.Fatalf("grants = %#v", grants)
	}
}

func TestReviewResolutionAppliesScopesBeforeLockValidation(t *testing.T) {
	lock := scopedGrantLock()
	plan := ResolutionPlan{Lock: lock}
	reviewed, err := reviewResolvedPluginPlan(plan, pkgOpts{scopes: map[string]map[string]project.AuthorityScope{
		"github.com/acme/plugin": {"host.import.define": {Modules: []string{"clock"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := reviewed.Lock
	if modules := got.Plugins["github.com/acme/plugin"].Grants[0].Scope.Modules; !reflect.DeepEqual(modules, []string{"clock"}) {
		t.Fatalf("modules = %v", modules)
	}
}
