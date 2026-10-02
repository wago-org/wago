package project

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testChecksum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func TestLockRoundTripV1CompleteResolution(t *testing.T) {
	dir := t.TempDir()
	got, err := ReadLock(dir)
	if err != nil || got.Plugins == nil || got.FormatVersion != 1 || len(got.Plugins) != 0 {
		t.Fatalf("ReadLock(empty) = %+v, %v", got, err)
	}
	want := NewLockDocument()
	want.Plugins["github.com/acme/pool"] = testLockEntry(true, "github.com/acme/pool", map[string]string{"github.com/acme/workers": "^1.0.0"})
	workers := testLockEntry(false, "github.com/acme/workers", map[string]string{})
	workers.RequestedAuthorities = []AuthorityRequest{{Name: "instance.manage", Mode: AuthorityRequired, Reason: "own the bounded worker instances", Scope: AuthorityScope{MaxInstances: 4, MaxMemoryBytes: 65536}}}
	workers.Grants = []AuthorityGrant{{Name: "instance.manage", Scope: AuthorityScope{MaxInstances: 4, MaxMemoryBytes: 65536}}}
	want.Plugins["github.com/acme/workers"] = workers
	if err := WriteLock(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err = ReadLock(dir)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("lock round trip:\n got %#v\nwant %#v\nerr %v", got, want, err)
	}
}

func TestReadLockRejectsV0UnknownAndMalformedState(t *testing.T) {
	for _, raw := range []string{
		`{"plugins":{"wago-org/wasi":{"version":"v0.0.0","capabilities":[]}}}`,
		`{"formatVersion":1,"plugins":[]}`,
		`{"formatVersion":2,"plugins":{}}`,
		`{"formatVersion":1,"plugins":{},"future":true}`,
		`{"formatVersion":1,"plugins":{}} {}`,
	} {
		dir := t.TempDir()
		if err := os.WriteFile(LockPath(dir), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadLock(dir); err == nil {
			t.Fatalf("ReadLock accepted invalid/v0 shape: %s", raw)
		}
	}
}

func TestLockAuthorityPolicyFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*LockEntry)
		want   string
	}{
		{"unknown", func(e *LockEntry) {
			e.RequestedAuthorities = []AuthorityRequest{{Name: "host.*", Mode: AuthorityOptional, Reason: "too broad"}}
		}, "unknown requested authority"},
		{"required denied", func(e *LockEntry) {
			e.RequestedAuthorities = []AuthorityRequest{{Name: "runtime.close.observe", Mode: AuthorityRequired, Reason: "flush state"}}
		}, "must be granted"},
		{"host wildcard", func(e *LockEntry) {
			e.RequestedAuthorities = []AuthorityRequest{{Name: "host.import.define", Mode: AuthorityOptional, Reason: "imports", Scope: AuthorityScope{Modules: []string{"*"}}}}
		}, "unique exact module names"},
		{"limit widened", func(e *LockEntry) {
			e.RequestedAuthorities = []AuthorityRequest{{Name: "instance.manage", Mode: AuthorityOptional, Reason: "pool", Scope: AuthorityScope{MaxInstances: 2, MaxMemoryBytes: 1024}}}
			e.Grants = []AuthorityGrant{{Name: "instance.manage", Scope: AuthorityScope{MaxInstances: 3, MaxMemoryBytes: 1024}}}
		}, "widens"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := NewLockDocument()
			entry := testLockEntry(true, "github.com/acme/plugin", map[string]string{})
			test.mutate(&entry)
			document.Plugins["github.com/acme/plugin"] = entry
			if _, err := EncodeLock(document); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("EncodeLock error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRequiredAuthorityMayBeGrantedWithNarrowerScope(t *testing.T) {
	document := NewLockDocument()
	entry := testLockEntry(true, "github.com/acme/plugin", map[string]string{})
	entry.RequestedAuthorities = []AuthorityRequest{{
		Name: "host.import.define", Mode: AuthorityRequired, Reason: "define clock and random imports",
		Scope: AuthorityScope{Modules: []string{"clock", "random"}},
	}}
	entry.Grants = []AuthorityGrant{{Name: "host.import.define", Scope: AuthorityScope{Modules: []string{"clock"}}}}
	document.Plugins["github.com/acme/plugin"] = entry
	if err := ValidateLock(document); err != nil {
		t.Fatalf("narrow required grant: %v", err)
	}
}

func TestModuleCloseAuthorityIsKnownAndUnscoped(t *testing.T) {
	document := NewLockDocument()
	entry := testLockEntry(true, "github.com/acme/plugin", map[string]string{})
	entry.RequestedAuthorities = []AuthorityRequest{{
		Name: "module.close.observe", Mode: AuthorityRequired, Reason: "release module metadata",
	}}
	entry.Grants = []AuthorityGrant{{Name: "module.close.observe"}}
	document.Plugins["github.com/acme/plugin"] = entry
	if err := ValidateLock(document); err != nil {
		t.Fatalf("module close authority: %v", err)
	}

	entry.Grants[0].Scope.Modules = []string{"env"}
	document.Plugins["github.com/acme/plugin"] = entry
	if err := ValidateLock(document); err == nil || !strings.Contains(err.Error(), "does not accept a scope") {
		t.Fatalf("scoped module close authority error=%v", err)
	}
}

func TestLockContractBindingsAndCycles(t *testing.T) {
	document := NewLockDocument()
	consumer := testLockEntry(true, "github.com/acme/pool", map[string]string{"github.com/acme/workers": "^1.0.0"})
	consumer.Contracts.Requires = []ContractRequirement{{ID: "github.com/acme/workers/service", Major: 1, Mode: "required"}}
	consumer.Bindings = []ContractBinding{{ID: "github.com/acme/workers/service", Major: 1, Providers: []string{"github.com/acme/workers"}}}
	provider := testLockEntry(false, "github.com/acme/workers", map[string]string{})
	provider.Contracts.Provides = []ContractProvider{{ID: "github.com/acme/workers/service", Major: 1}}
	document.Plugins["github.com/acme/pool"] = consumer
	document.Plugins["github.com/acme/workers"] = provider
	if err := ValidateLock(document); err != nil {
		t.Fatalf("valid contract graph: %v", err)
	}

	consumer.Bindings[0].Providers[0] = "github.com/acme/missing"
	document.Plugins["github.com/acme/pool"] = consumer
	if err := ValidateLock(document); err == nil || !strings.Contains(err.Error(), "does not provide") {
		t.Fatalf("missing binding provider error = %v", err)
	}
	consumer.Bindings[0].Providers[0] = "github.com/acme/workers"
	provider.Dependencies["github.com/acme/pool"] = "^1.0.0"
	document.Plugins["github.com/acme/pool"] = consumer
	document.Plugins["github.com/acme/workers"] = provider
	if err := ValidateLock(document); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestOptionalContractMayBindNoProvider(t *testing.T) {
	document := NewLockDocument()
	entry := testLockEntry(true, "github.com/acme/metrics", map[string]string{})
	entry.Contracts.Requires = []ContractRequirement{{ID: "github.com/acme/tracing/service", Major: 1, Mode: "optional"}}
	entry.Bindings = []ContractBinding{{ID: "github.com/acme/tracing/service", Major: 1, Providers: []string{}}}
	document.Plugins["github.com/acme/metrics"] = entry
	if err := ValidateLock(document); err != nil {
		t.Fatal(err)
	}
}

func TestManyContractMustBindEverySelectedProvider(t *testing.T) {
	document := NewLockDocument()
	consumer := testLockEntry(true, "github.com/acme/consumer", map[string]string{
		"github.com/acme/a": "*",
		"github.com/acme/b": "*",
	})
	consumer.Contracts.Requires = []ContractRequirement{{ID: "github.com/acme/service", Major: 1, Mode: "many"}}
	consumer.Bindings = []ContractBinding{{ID: "github.com/acme/service", Major: 1, Providers: []string{"github.com/acme/b"}}}
	for _, id := range []string{"github.com/acme/a", "github.com/acme/b"} {
		provider := testLockEntry(false, id, map[string]string{})
		provider.Contracts.Provides = []ContractProvider{{ID: "github.com/acme/service", Major: 1}}
		document.Plugins[id] = provider
	}
	document.Plugins["github.com/acme/consumer"] = consumer
	if err := ValidateLock(document); err == nil || !strings.Contains(err.Error(), "must include every selected provider") {
		t.Fatalf("incomplete many binding error = %v", err)
	}

	consumer.Bindings[0].Providers = []string{"github.com/acme/b", "github.com/acme/a"}
	document.Plugins["github.com/acme/consumer"] = consumer
	if err := ValidateLock(document); err != nil {
		t.Fatalf("complete many binding in reviewed order: %v", err)
	}
}

func TestLockContractBindingIsExactChoiceNotEveryAvailableProvider(t *testing.T) {
	document := NewLockDocument()
	consumer := testLockEntry(true, "github.com/acme/consumer", map[string]string{"github.com/acme/a": "*", "github.com/acme/b": "*"})
	consumer.Contracts.Requires = []ContractRequirement{{ID: "github.com/acme/service", Major: 1, Mode: "required"}}
	consumer.Bindings = []ContractBinding{{ID: "github.com/acme/service", Major: 1, Providers: []string{"github.com/acme/b"}}}
	for _, id := range []string{"github.com/acme/a", "github.com/acme/b"} {
		provider := testLockEntry(false, id, map[string]string{})
		provider.Contracts.Provides = []ContractProvider{{ID: "github.com/acme/service", Major: 1}}
		document.Plugins[id] = provider
	}
	document.Plugins["github.com/acme/consumer"] = consumer
	if err := ValidateLock(document); err != nil {
		t.Fatalf("exact reviewed binding among multiple providers: %v", err)
	}
}

func TestValidateLockedResolutionIsCompletePrunedAndReproducible(t *testing.T) {
	requirements := []PluginRequirement{{ID: "github.com/acme/pool", Constraint: "^1.0.0"}}
	document := NewLockDocument()
	document.Plugins["github.com/acme/pool"] = testLockEntry(true, "github.com/acme/pool", map[string]string{"github.com/acme/workers": ">=1.0.0 <2.0.0"})
	document.Plugins["github.com/acme/workers"] = testLockEntry(false, "github.com/acme/workers", map[string]string{})
	if err := ValidateLockedResolution(requirements, document); err != nil {
		t.Fatal(err)
	}
	extra := testLockEntry(false, "github.com/acme/stale", map[string]string{})
	document.Plugins["github.com/acme/stale"] = extra
	if err := ValidateLock(document); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("base lock unreachable transitive error = %v", err)
	}
	if err := ValidateLockedResolution(requirements, document); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("unreachable transitive error = %v", err)
	}
	delete(document.Plugins, "github.com/acme/stale")
	workers := document.Plugins["github.com/acme/workers"]
	workers.Source.Version = "v2.0.0"
	document.Plugins["github.com/acme/workers"] = workers
	if err := ValidateLockedResolution(requirements, document); err == nil || !strings.Contains(err.Error(), "does not satisfy") {
		t.Fatalf("constraint drift error = %v", err)
	}
}

func TestValidateLockedResolutionIncludesContractReachability(t *testing.T) {
	const consumerID = "github.com/acme/consumer"
	const providerID = "github.com/acme/provider"
	const contractID = "github.com/acme/service"
	requirements := []PluginRequirement{{ID: consumerID, Constraint: "^1.0.0"}}
	document := NewLockDocument()
	consumer := testLockEntry(true, consumerID, map[string]string{})
	consumer.Contracts.Requires = []ContractRequirement{{ID: contractID, Major: 1, Mode: "required"}}
	consumer.Bindings = []ContractBinding{{ID: contractID, Major: 1, Providers: []string{providerID}}}
	provider := testLockEntry(false, providerID, map[string]string{})
	provider.Contracts.Provides = []ContractProvider{{ID: contractID, Major: 1}}
	document.Plugins[consumerID] = consumer
	document.Plugins[providerID] = provider

	if err := ValidateLockedResolution(requirements, document); err != nil {
		t.Fatalf("contract-reachable provider rejected: %v", err)
	}
}

func TestLockRejectsEmptyDependencyConstraint(t *testing.T) {
	document := NewLockDocument()
	entry := testLockEntry(true, "github.com/acme/consumer", map[string]string{"github.com/acme/dependency": "  "})
	document.Plugins["github.com/acme/consumer"] = entry
	document.Plugins["github.com/acme/dependency"] = testLockEntry(false, "github.com/acme/dependency", map[string]string{})
	if err := ValidateLock(document); err == nil || !strings.Contains(err.Error(), "invalid version constraint") {
		t.Fatalf("empty locked dependency constraint error = %v", err)
	}
}

func TestLockRejectsConflictingSharedSourceReleases(t *testing.T) {
	const moduleID = "github.com/acme/bundle"
	newDocument := func() LockDocument {
		document := NewLockDocument()
		for _, suffix := range []string{"alpha", "beta"} {
			id := moduleID + "/" + suffix
			entry := testLockEntry(true, id, map[string]string{})
			entry.Source.Module = moduleID
			entry.Provider.ImportPath = moduleID + "/register"
			document.Plugins[id] = entry
		}
		return document
	}
	tests := []struct {
		name   string
		mutate func(*LockEntry)
	}{
		{name: "version", mutate: func(entry *LockEntry) { entry.Source.Version = "v1.2.4" }},
		{name: "checksum", mutate: func(entry *LockEntry) { entry.Source.Checksum = "h1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=" }},
		{name: "release fingerprint", mutate: func(entry *LockEntry) {
			entry.ReleaseFingerprint = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := newDocument()
			entry := document.Plugins[moduleID+"/beta"]
			test.mutate(&entry)
			document.Plugins[moduleID+"/beta"] = entry
			if err := ValidateLock(document); err == nil || !strings.Contains(err.Error(), "conflicting release") || !strings.Contains(err.Error(), moduleID) {
				t.Fatalf("shared source conflict error = %v", err)
			}
		})
	}
}

func TestValidGoChecksumRequiresCanonicalBase64(t *testing.T) {
	canonical := "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	nonCanonical := canonical[:len(canonical)-2] + "B="
	if !ValidGoChecksum(canonical) {
		t.Fatal("canonical Go checksum was rejected")
	}
	if ValidGoChecksum(nonCanonical) {
		t.Fatal("Go checksum with nonzero Base64 padding bits was accepted")
	}
	for _, checksum := range []string{canonical + "A", strings.TrimSuffix(canonical, "=")} {
		if ValidGoChecksum(checksum) {
			t.Fatalf("wrong-length Go checksum %q was accepted", checksum)
		}
	}
}

func testLockEntry(direct bool, id string, dependencies map[string]string) LockEntry {
	return LockEntry{
		Direct:               direct,
		Source:               PluginSource{Module: id, Version: "v1.2.3", Checksum: testChecksum},
		Provider:             ProviderSource{ImportPath: id + "/register"},
		DefinitionDigest:     testDigest,
		ReleaseFingerprint:   testDigest,
		Dependencies:         dependencies,
		RequestedAuthorities: []AuthorityRequest{}, Grants: []AuthorityGrant{},
		Contracts: ContractSet{Provides: []ContractProvider{}, Requires: []ContractRequirement{}},
		Bindings:  []ContractBinding{}, Config: json.RawMessage(`{}`),
	}
}
