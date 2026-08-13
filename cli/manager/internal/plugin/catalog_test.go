package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"
	corewago "github.com/wago-org/wago/src/wago"
)

func TestResolveCatalogGraphPoolWorkersDependencyAndBinding(t *testing.T) {
	workersID := "github.com/wago-org/workers"
	poolID := "github.com/JairusSW/pool"
	workers := testCatalogRelease(t, workersID, "0.1.0")
	workers.Definition.Provides = []corewago.ContractSpec{{ID: workersID + "/service", Major: 1}}
	workers = resignRelease(t, workers)
	pool := testCatalogRelease(t, poolID, "0.1.0")
	pool.Definition.Requires = []corewago.PluginRequirement{{ID: workersID, Version: "^0.1.0"}}
	pool.Definition.Consumes = []corewago.ContractRequirement{{ID: workersID + "/service", Major: 1, Mode: corewago.ContractRequired}}
	pool = resignRelease(t, pool)
	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{poolID: {pool}, workersID: {workers}}}
	plan, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: poolID, Constraint: "^0.1.0"}}, project.NewLockDocument())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Lock.Plugins) != 2 || !plan.Lock.Plugins[poolID].Direct || plan.Lock.Plugins[workersID].Direct {
		t.Fatalf("direct/transitive graph = %#v", plan.Lock.Plugins)
	}
	want := []string{workersID}
	if got := plan.Lock.Plugins[poolID].Bindings[0].Providers; !reflect.DeepEqual(got, want) {
		t.Fatalf("contract binding = %v, want %v", got, want)
	}
	if got := plan.Lock.Plugins[poolID].Dependencies[workersID]; got != "^0.1.0" {
		t.Fatalf("Workers dependency = %q, want ^0.1.0", got)
	}
	if got := plan.Lock.Plugins[workersID].Source.Version; got != "v0.1.0" {
		t.Fatalf("Workers release = %q, want v0.1.0", got)
	}
	if err := project.ValidateLockedResolution([]project.PluginRequirement{{ID: poolID, Constraint: "^0.1.0"}}, plan.Lock); err != nil {
		t.Fatalf("resolved lock is not reproducible: %v", err)
	}
}

func TestResolveCatalogGraphRecordsExactReviewedContractChoice(t *testing.T) {
	contractID := "github.com/acme/service"
	consumerID := "github.com/acme/consumer"
	providerAID, providerBID := "github.com/acme/a", "github.com/acme/b"
	consumer := testCatalogRelease(t, consumerID, "1.0.0")
	consumer.Definition.Requires = []corewago.PluginRequirement{{ID: providerAID, Version: "*"}, {ID: providerBID, Version: "*"}}
	consumer.Definition.Consumes = []corewago.ContractRequirement{{ID: contractID, Major: 1, Mode: corewago.ContractRequired}}
	consumer = resignRelease(t, consumer)
	providerA := testCatalogRelease(t, providerAID, "1.0.0")
	providerA.Definition.Provides = []corewago.ContractSpec{{ID: contractID, Major: 1}}
	providerA = resignRelease(t, providerA)
	providerB := testCatalogRelease(t, providerBID, "1.0.0")
	providerB.Definition.Provides = []corewago.ContractSpec{{ID: contractID, Major: 1}}
	providerB = resignRelease(t, providerB)
	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{consumerID: {consumer}, providerAID: {providerA}, providerBID: {providerB}}}
	plan, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: consumerID, Constraint: "*"}}, project.NewLockDocument())
	if err != nil {
		t.Fatal(err)
	}
	binding := plan.Lock.Plugins[consumerID].Bindings[0]
	if !reflect.DeepEqual(binding.Providers, []string{providerAID}) {
		t.Fatalf("binding = %#v, want one exact deterministic provider", binding)
	}
	if len(plan.ContractReviews) != 1 || !reflect.DeepEqual(plan.ContractReviews[0].Available, []string{providerAID, providerBID}) {
		t.Fatalf("contract review = %#v", plan.ContractReviews)
	}

	previous := plan.Lock
	entry := previous.Plugins[consumerID]
	entry.Bindings[0].Providers = []string{providerBID}
	previous.Plugins[consumerID] = entry
	plan, err = ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: consumerID, Constraint: "*"}}, previous)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Lock.Plugins[consumerID].Bindings[0].Providers; !reflect.DeepEqual(got, []string{providerBID}) {
		t.Fatalf("preserved reviewed binding = %#v", got)
	}
}

func TestResolveCatalogGraphBacktracksContractBindingAroundCycle(t *testing.T) {
	contractID := "github.com/acme/service"
	consumerID := "github.com/acme/consumer"
	providerAID, providerBID := "github.com/acme/a", "github.com/acme/b"

	consumer := testCatalogRelease(t, consumerID, "1.0.0")
	consumer.Definition.Consumes = []corewago.ContractRequirement{{
		ID: contractID, Major: 1, Mode: corewago.ContractRequired,
	}}
	consumer = resignRelease(t, consumer)

	providerA := testCatalogRelease(t, providerAID, "1.0.0")
	providerA.Definition.Requires = []corewago.PluginRequirement{{ID: consumerID, Version: "*"}}
	providerA.Definition.Provides = []corewago.ContractSpec{{ID: contractID, Major: 1}}
	providerA = resignRelease(t, providerA)

	providerB := testCatalogRelease(t, providerBID, "1.0.0")
	providerB.Definition.Provides = []corewago.ContractSpec{{ID: contractID, Major: 1}}
	providerB = resignRelease(t, providerB)

	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{
		consumerID:  {consumer},
		providerAID: {providerA},
		providerBID: {providerB},
	}}
	plan, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{
		{ID: consumerID, Constraint: "*"},
		{ID: providerAID, Constraint: "*"},
		{ID: providerBID, Constraint: "*"},
	}, project.NewLockDocument())
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Lock.Plugins[consumerID].Bindings[0].Providers; !reflect.DeepEqual(got, []string{providerBID}) {
		t.Fatalf("binding = %#v, want acyclic provider %#v", got, []string{providerBID})
	}
}

func TestResolveCatalogGraphBacktracksReleaseWhenNewestCannotBindAcyclically(t *testing.T) {
	contractID := "github.com/acme/service"
	consumerID, providerID := "github.com/acme/consumer", "github.com/acme/provider"
	consumer := testCatalogRelease(t, consumerID, "1.0.0")
	consumer.Definition.Consumes = []corewago.ContractRequirement{{
		ID: contractID, Major: 1, Mode: corewago.ContractRequired,
	}}
	consumer = resignRelease(t, consumer)
	newest := testCatalogRelease(t, providerID, "2.0.0")
	newest.Definition.Requires = []corewago.PluginRequirement{{ID: consumerID, Version: "*"}}
	newest.Definition.Provides = []corewago.ContractSpec{{ID: contractID, Major: 1}}
	newest = resignRelease(t, newest)
	older := testCatalogRelease(t, providerID, "1.0.0")
	older.Definition.Provides = []corewago.ContractSpec{{ID: contractID, Major: 1}}
	older = resignRelease(t, older)
	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{
		consumerID: {consumer}, providerID: {newest, older},
	}}
	plan, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{
		{ID: consumerID, Constraint: "*"}, {ID: providerID, Constraint: "*"},
	}, project.NewLockDocument())
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Lock.Plugins[providerID].Source.Version; got != "v1.0.0" {
		t.Fatalf("provider version = %s, want acyclic v1.0.0", got)
	}
}

func TestResolveCatalogGraphMovesPreviousOptionalBindingAroundCycle(t *testing.T) {
	contractID := "github.com/acme/service"
	consumerID := "github.com/acme/consumer"
	providerAID, providerBID := "github.com/acme/a", "github.com/acme/b"
	consumer := testCatalogRelease(t, consumerID, "1.0.0")
	consumer.Definition.Consumes = []corewago.ContractRequirement{{
		ID: contractID, Major: 1, Mode: corewago.ContractOptional,
	}}
	consumer = resignRelease(t, consumer)
	providerA := testCatalogRelease(t, providerAID, "1.0.0")
	providerA.Definition.Requires = []corewago.PluginRequirement{{ID: consumerID, Version: "*"}}
	providerA.Definition.Provides = []corewago.ContractSpec{{ID: contractID, Major: 1}}
	providerA = resignRelease(t, providerA)
	providerB := testCatalogRelease(t, providerBID, "1.0.0")
	providerB.Definition.Provides = []corewago.ContractSpec{{ID: contractID, Major: 1}}
	providerB = resignRelease(t, providerB)
	previous := project.NewLockDocument()
	previous.Plugins[consumerID] = project.LockEntry{Bindings: []project.ContractBinding{{
		ID: contractID, Major: 1, Providers: []string{providerAID},
	}}}
	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{
		consumerID: {consumer}, providerAID: {providerA}, providerBID: {providerB},
	}}
	plan, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{
		{ID: consumerID, Constraint: "*"},
		{ID: providerAID, Constraint: "*"},
		{ID: providerBID, Constraint: "*"},
	}, previous)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Lock.Plugins[consumerID].Bindings[0].Providers; !reflect.DeepEqual(got, []string{providerBID}) {
		t.Fatalf("optional binding = %#v, want acyclic alternate %#v", got, []string{providerBID})
	}
	if len(plan.ContractReviews) != 1 || plan.ContractReviews[0].Change != "changed" {
		t.Fatalf("optional binding review = %#v", plan.ContractReviews)
	}
}

func TestResolveCatalogGraphManyBindsAllAndPreservesSurvivorOrder(t *testing.T) {
	contractID := "github.com/acme/service"
	consumerID := "github.com/acme/consumer"
	providerAID, providerBID := "github.com/acme/a", "github.com/acme/b"
	providerDID, removedID := "github.com/acme/d", "github.com/acme/removed"
	consumer := testCatalogRelease(t, consumerID, "1.0.0")
	for _, id := range []string{providerAID, providerBID, providerDID} {
		consumer.Definition.Requires = append(consumer.Definition.Requires, corewago.PluginRequirement{ID: id, Version: "*"})
	}
	consumer.Definition.Consumes = []corewago.ContractRequirement{{ID: contractID, Major: 1, Mode: corewago.ContractMany}}
	consumer = resignRelease(t, consumer)
	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{consumerID: {consumer}}}
	for _, id := range []string{providerAID, providerBID, providerDID} {
		provider := testCatalogRelease(t, id, "1.0.0")
		provider.Definition.Provides = []corewago.ContractSpec{{ID: contractID, Major: 1}}
		catalog.Releases[id] = []CatalogRelease{resignRelease(t, provider)}
	}

	plan, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: consumerID, Constraint: "*"}}, project.NewLockDocument())
	if err != nil {
		t.Fatal(err)
	}
	wantFresh := []string{providerAID, providerBID, providerDID}
	if got := plan.Lock.Plugins[consumerID].Bindings[0].Providers; !reflect.DeepEqual(got, wantFresh) {
		t.Fatalf("fresh many binding = %#v, want %#v", got, wantFresh)
	}
	if len(plan.ContractReviews) != 1 || !reflect.DeepEqual(plan.ContractReviews[0].Proposed, wantFresh) {
		t.Fatalf("fresh many review = %#v", plan.ContractReviews)
	}

	previous := project.NewLockDocument()
	previous.Plugins[consumerID] = project.LockEntry{Bindings: []project.ContractBinding{{
		ID: contractID, Major: 1, Providers: []string{removedID, providerBID, providerAID},
	}}}
	plan, err = ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: consumerID, Constraint: "*"}}, previous)
	if err != nil {
		t.Fatal(err)
	}
	wantUpdated := []string{providerBID, providerAID, providerDID}
	if got := plan.Lock.Plugins[consumerID].Bindings[0].Providers; !reflect.DeepEqual(got, wantUpdated) {
		t.Fatalf("updated many binding = %#v, want surviving reviewed order plus additions %#v", got, wantUpdated)
	}
	if len(plan.ContractReviews) != 1 || plan.ContractReviews[0].Change != "changed" ||
		!reflect.DeepEqual(plan.ContractReviews[0].Previous, []string{removedID, providerBID, providerAID}) ||
		!reflect.DeepEqual(plan.ContractReviews[0].Proposed, wantUpdated) {
		t.Fatalf("updated many review = %#v", plan.ContractReviews)
	}
}

func TestResolveCatalogGraphNewOptionalWithProvidersRequiresReview(t *testing.T) {
	automation.Reset()
	automation.Configure(automation.Options{NoInput: true})
	t.Cleanup(automation.Reset)
	contractID := "github.com/acme/service"
	consumerID, providerID := "github.com/acme/consumer", "github.com/acme/provider"
	consumer := testCatalogRelease(t, consumerID, "1.0.0")
	consumer.Definition.Requires = []corewago.PluginRequirement{{ID: providerID, Version: "*"}}
	consumer.Definition.Consumes = []corewago.ContractRequirement{{ID: contractID, Major: 1, Mode: corewago.ContractOptional}}
	consumer = resignRelease(t, consumer)
	provider := testCatalogRelease(t, providerID, "1.0.0")
	provider.Definition.Provides = []corewago.ContractSpec{{ID: contractID, Major: 1}}
	provider = resignRelease(t, provider)
	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{consumerID: {consumer}, providerID: {provider}}}

	plan, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: consumerID, Constraint: "*"}}, project.NewLockDocument())
	if err != nil {
		t.Fatal(err)
	}
	binding := plan.Lock.Plugins[consumerID].Bindings[0]
	if binding.Providers == nil || len(binding.Providers) != 0 {
		t.Fatalf("new optional binding = %#v, want explicit no-provider proposal", binding)
	}
	if len(plan.ContractReviews) != 1 || plan.ContractReviews[0].Change != "new" ||
		!reflect.DeepEqual(plan.ContractReviews[0].Available, []string{providerID}) || len(plan.ContractReviews[0].Proposed) != 0 {
		t.Fatalf("new optional review = %#v", plan.ContractReviews)
	}
	if _, err := reviewResolution(plan, pkgOpts{}); err == nil || !strings.Contains(err.Error(), "--accept-contracts") {
		t.Fatalf("unaccepted optional contract review error = %v", err)
	}
	accepted, err := reviewResolution(plan, pkgOpts{acceptContracts: true})
	if err != nil {
		t.Fatalf("accept optional no-provider proposal: %v", err)
	}
	if got := accepted.Plugins[consumerID].Bindings[0].Providers; got == nil || len(got) != 0 {
		t.Fatalf("accepted optional binding = %#v", got)
	}
	plan, err = ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: consumerID, Constraint: "*"}}, accepted)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ContractReviews) != 0 {
		t.Fatalf("reviewed optional absence prompted again: %#v", plan.ContractReviews)
	}
}

func TestResolveCatalogGraphRejectsEmptyDependencyConstraint(t *testing.T) {
	consumerID, dependencyID := "github.com/acme/consumer", "github.com/acme/dependency"
	consumer := testCatalogRelease(t, consumerID, "1.0.0")
	consumer.Definition.Requires = []corewago.PluginRequirement{{ID: dependencyID, Version: ""}}
	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{consumerID: {consumer}}}
	_, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: consumerID, Constraint: "*"}}, project.NewLockDocument())
	if err == nil || !strings.Contains(err.Error(), "version constraint is empty") {
		t.Fatalf("empty dependency constraint error = %v", err)
	}
}

func TestContractChangesRequireExplicitNoninteractiveReview(t *testing.T) {
	automation.Reset()
	automation.Configure(automation.Options{NoInput: true})
	t.Cleanup(automation.Reset)
	id := "github.com/acme/consumer"
	entry := testManagerLockEntry(id)
	entry.Contracts.Requires = []project.ContractRequirement{{ID: "github.com/acme/service", Major: 1, Mode: "optional"}}
	entry.Bindings = []project.ContractBinding{{ID: "github.com/acme/service", Major: 1, Providers: []string{}}}
	lock := project.NewLockDocument()
	lock.Plugins[id] = entry
	plan := ResolutionPlan{Lock: lock, ContractReviews: []ContractReview{{
		PluginID: id, Request: entry.Contracts.Requires[0], Proposed: []string{}, Change: "new",
	}}}
	if _, err := reviewResolution(plan, pkgOpts{}); err == nil || !strings.Contains(err.Error(), "--accept-contracts") {
		t.Fatalf("unreviewed contract error = %v", err)
	}
	if _, err := reviewResolution(plan, pkgOpts{acceptContracts: true}); err != nil {
		t.Fatalf("explicit contract review: %v", err)
	}
}

func TestRemovalDoesNotSilentlyAcceptContractChanges(t *testing.T) {
	automation.Reset()
	automation.Configure(automation.Options{NoInput: true})
	t.Cleanup(automation.Reset)
	id := "github.com/acme/consumer"
	entry := testManagerLockEntry(id)
	entry.Contracts.Requires = []project.ContractRequirement{{ID: "github.com/acme/service", Major: 1, Mode: "many"}}
	entry.Bindings = []project.ContractBinding{{ID: "github.com/acme/service", Major: 1, Providers: []string{"github.com/acme/remaining"}}}
	lock := project.NewLockDocument()
	lock.Plugins[id] = entry
	remaining := testManagerLockEntry("github.com/acme/remaining")
	remaining.Direct = false
	remaining.Contracts.Provides = []project.ContractProvider{{ID: "github.com/acme/service", Major: 1}}
	lock.Plugins["github.com/acme/remaining"] = remaining
	plan := ResolutionPlan{Lock: lock, ContractReviews: []ContractReview{{
		PluginID: id, Request: entry.Contracts.Requires[0],
		Previous: []string{"github.com/acme/removed", "github.com/acme/remaining"},
		Proposed: []string{"github.com/acme/remaining"}, Change: "changed",
	}}}
	if _, err := reviewRemovalResolution(plan, pkgOpts{}); err == nil || !strings.Contains(err.Error(), "--accept-contracts") {
		t.Fatalf("unreviewed removal error = %v", err)
	}
	if _, err := reviewRemovalResolution(plan, pkgOpts{acceptContracts: true}); err != nil {
		t.Fatalf("explicit removal contract review: %v", err)
	}
}

func TestResolveCatalogGraphDiamondDeduplicatesAndIntersectsRanges(t *testing.T) {
	commonID := "github.com/acme/common"
	left := testCatalogRelease(t, "github.com/acme/left", "1.0.0")
	left.Definition.Requires = []corewago.PluginRequirement{{ID: commonID, Version: ">=1.0.0 <3.0.0"}}
	left = resignRelease(t, left)
	right := testCatalogRelease(t, "github.com/acme/right", "1.0.0")
	right.Definition.Requires = []corewago.PluginRequirement{{ID: commonID, Version: "^1.0.0"}}
	right = resignRelease(t, right)
	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{
		left.ID: {left}, right.ID: {right}, commonID: {testCatalogRelease(t, commonID, "2.1.0"), testCatalogRelease(t, commonID, "1.5.0")},
	}}
	plan, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: left.ID, Constraint: "*"}, {ID: right.ID, Constraint: "*"}}, project.NewLockDocument())
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Lock.Plugins[commonID].Source.Version; got != "v1.5.0" {
		t.Fatalf("common version = %s", got)
	}
	if len(plan.Lock.Plugins) != 3 {
		t.Fatalf("diamond produced %d resolutions", len(plan.Lock.Plugins))
	}
	last := catalog.Calls[len(catalog.Calls)-1]
	if last.ID != commonID || !reflect.DeepEqual(last.Constraints, []string{">=1.0.0 <3.0.0", "^1.0.0"}) {
		t.Fatalf("last catalog call = %#v", last)
	}
}

func TestResolveCatalogGraphRejectsConflictsAndCycles(t *testing.T) {
	commonID := "github.com/acme/common"
	left := testCatalogRelease(t, "github.com/acme/left", "1.0.0")
	left.Definition.Requires = []corewago.PluginRequirement{{ID: commonID, Version: "^1.0.0"}}
	left = resignRelease(t, left)
	right := testCatalogRelease(t, "github.com/acme/right", "1.0.0")
	right.Definition.Requires = []corewago.PluginRequirement{{ID: commonID, Version: "^2.0.0"}}
	right = resignRelease(t, right)
	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{
		left.ID: {left}, right.ID: {right}, commonID: {testCatalogRelease(t, commonID, "1.2.0"), testCatalogRelease(t, commonID, "2.2.0")},
	}}
	_, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: left.ID, Constraint: "*"}, {ID: right.ID, Constraint: "*"}}, project.NewLockDocument())
	if err == nil || (!strings.Contains(err.Error(), "conflicts") && !strings.Contains(err.Error(), "no release satisfying")) {
		t.Fatalf("conflict error = %v", err)
	}

	a := testCatalogRelease(t, "github.com/acme/a", "1.0.0")
	b := testCatalogRelease(t, "github.com/acme/b", "1.0.0")
	a.Definition.Requires = []corewago.PluginRequirement{{ID: b.ID, Version: "*"}}
	b.Definition.Requires = []corewago.PluginRequirement{{ID: a.ID, Version: "*"}}
	a, b = resignRelease(t, a), resignRelease(t, b)
	catalog = &MemoryCatalog{Releases: map[string][]CatalogRelease{a.ID: {a}, b.ID: {b}}}
	_, err = ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: a.ID, Constraint: "*"}}, project.NewLockDocument())
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestResolveCatalogGraphBacktracksFromNewestCandidate(t *testing.T) {
	xID := "github.com/acme/x"
	aID := "github.com/acme/a"
	bID := "github.com/acme/b"
	a2 := testCatalogRelease(t, aID, "2.0.0")
	a2.Definition.Requires = []corewago.PluginRequirement{{ID: xID, Version: "^2.0.0"}}
	a2 = resignRelease(t, a2)
	a1 := testCatalogRelease(t, aID, "1.0.0")
	a1.Definition.Requires = []corewago.PluginRequirement{{ID: xID, Version: "^1.0.0"}}
	a1 = resignRelease(t, a1)
	b := testCatalogRelease(t, bID, "1.0.0")
	b.Definition.Requires = []corewago.PluginRequirement{{ID: xID, Version: "^1.0.0"}}
	b = resignRelease(t, b)
	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{
		aID: {a2, a1}, bID: {b}, xID: {testCatalogRelease(t, xID, "2.0.0"), testCatalogRelease(t, xID, "1.0.0")},
	}}
	plan, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: aID, Constraint: "*"}, {ID: bID, Constraint: "*"}}, project.NewLockDocument())
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Lock.Plugins[aID].Source.Version; got != "v1.0.0" {
		t.Fatalf("a = %s, want globally compatible v1.0.0", got)
	}
	if got := plan.Lock.Plugins[xID].Source.Version; got != "v1.0.0" {
		t.Fatalf("x = %s, want v1.0.0", got)
	}
}

func TestResolveCatalogGraphBacktracksToCommonSharedSourceRelease(t *testing.T) {
	moduleID := "github.com/acme/bundle"
	alphaID, betaID := moduleID+"/alpha", moduleID+"/beta"
	alpha2 := testSharedSourceRelease(t, alphaID, moduleID, "2.0.0", "h1:AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=", "sha256:2222222222222222222222222222222222222222222222222222222222222222")
	alpha1 := testSharedSourceRelease(t, alphaID, moduleID, "1.0.0", "h1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", "sha256:1111111111111111111111111111111111111111111111111111111111111111")
	beta3 := testSharedSourceRelease(t, betaID, moduleID, "3.0.0", "h1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=", "sha256:3333333333333333333333333333333333333333333333333333333333333333")
	beta1 := testSharedSourceRelease(t, betaID, moduleID, "1.0.0", "h1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", "sha256:1111111111111111111111111111111111111111111111111111111111111111")
	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{
		alphaID: {alpha2, alpha1},
		betaID:  {beta3, beta1},
	}}

	plan, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{
		{ID: alphaID, Constraint: "*"},
		{ID: betaID, Constraint: "*"},
	}, project.NewLockDocument())
	if err != nil {
		t.Fatal(err)
	}
	alpha, beta := plan.Lock.Plugins[alphaID], plan.Lock.Plugins[betaID]
	if alpha.Source.Version != "v1.0.0" || beta.Source.Version != "v1.0.0" {
		t.Fatalf("shared source versions = %s/%s, want common v1.0.0", alpha.Source.Version, beta.Source.Version)
	}
	if alpha.Source != beta.Source || alpha.ReleaseFingerprint != beta.ReleaseFingerprint {
		t.Fatalf("shared source release drifted: alpha=%#v/%s beta=%#v/%s", alpha.Source, alpha.ReleaseFingerprint, beta.Source, beta.ReleaseFingerprint)
	}
}

func TestResolveCatalogGraphRejectsImpossibleSharedSourceRelease(t *testing.T) {
	moduleID := "github.com/acme/bundle"
	alphaID, betaID := moduleID+"/alpha", moduleID+"/beta"
	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{
		alphaID: {testSharedSourceRelease(t, alphaID, moduleID, "2.0.0", "h1:AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=", "sha256:2222222222222222222222222222222222222222222222222222222222222222")},
		betaID:  {testSharedSourceRelease(t, betaID, moduleID, "3.0.0", "h1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=", "sha256:3333333333333333333333333333333333333333333333333333333333333333")},
	}}

	_, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{
		{ID: alphaID, Constraint: "*"},
		{ID: betaID, Constraint: "*"},
	}, project.NewLockDocument())
	if err == nil || !strings.Contains(err.Error(), "conflicting release") || !strings.Contains(err.Error(), moduleID) {
		t.Fatalf("impossible shared source error = %v", err)
	}
}

func TestResolveCatalogGraphOptionalContractAbsentAndPrunesStaleDependencies(t *testing.T) {
	optional := testCatalogRelease(t, "github.com/acme/metrics", "1.0.0")
	optional.Definition.Consumes = []corewago.ContractRequirement{{ID: "github.com/acme/tracing/service", Major: 1, Mode: corewago.ContractOptional}}
	optional = resignRelease(t, optional)
	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{optional.ID: {optional}}}
	plan, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: optional.ID, Constraint: "*"}}, project.NewLockDocument())
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Lock.Plugins[optional.ID].Bindings; len(got) != 1 || got[0].Providers == nil || len(got[0].Providers) != 0 {
		t.Fatalf("optional absent binding = %#v", got)
	}

	parentID, limiterID := "github.com/acme/parent", "github.com/acme/limiter"
	oldID, nextID := "github.com/acme/old", "github.com/acme/next"
	parent2 := testCatalogRelease(t, parentID, "2.0.0")
	parent2.Definition.Requires = []corewago.PluginRequirement{{ID: oldID, Version: "*"}}
	parent2 = resignRelease(t, parent2)
	parent1 := testCatalogRelease(t, parentID, "1.0.0")
	parent1.Definition.Requires = []corewago.PluginRequirement{{ID: nextID, Version: "*"}}
	parent1 = resignRelease(t, parent1)
	limiter := testCatalogRelease(t, limiterID, "1.0.0")
	limiter.Definition.Requires = []corewago.PluginRequirement{{ID: parentID, Version: "<2.0.0"}}
	limiter = resignRelease(t, limiter)
	catalog = &MemoryCatalog{Releases: map[string][]CatalogRelease{
		parentID: {parent2, parent1}, limiterID: {limiter}, oldID: {testCatalogRelease(t, oldID, "1.0.0")}, nextID: {testCatalogRelease(t, nextID, "1.0.0")},
	}}
	plan, err = ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: parentID, Constraint: ">=1.0.0 <3.0.0"}, {ID: limiterID, Constraint: "*"}}, project.NewLockDocument())
	if err != nil {
		t.Fatal(err)
	}
	if _, stale := plan.Lock.Plugins[oldID]; stale {
		t.Fatalf("stale transitive resolution survived: %#v", plan.Lock.Plugins)
	}
	if _, ok := plan.Lock.Plugins[nextID]; !ok {
		t.Fatalf("new transitive resolution missing: %#v", plan.Lock.Plugins)
	}
}

func TestResolveCatalogGraphAuthorityReviewPreservesNarrowOptionalGrant(t *testing.T) {
	id := "github.com/acme/imports"
	release := testCatalogRelease(t, id, "1.1.0")
	release.Definition.Authorities = []corewago.AuthorityRequest{{
		Name: corewago.AuthorityHostImportDefine, Mode: corewago.AuthorityOptional,
		Reason: "offer clock and random host imports", Scope: corewago.AuthorityScope{Modules: []string{"clock", "random"}},
	}}
	release = resignRelease(t, release)
	previous := project.NewLockDocument()
	previous.Plugins[id] = project.LockEntry{
		RequestedAuthorities: []project.AuthorityRequest{{Name: "host.import.define", Mode: "optional", Reason: "old", Scope: project.AuthorityScope{Modules: []string{"clock"}}}},
		Grants:               []project.AuthorityGrant{{Name: "host.import.define", Scope: project.AuthorityScope{Modules: []string{"clock"}}}},
		Config:               json.RawMessage(`{"mode":"safe"}`),
	}
	plan, err := ResolveCatalogGraph(context.Background(), &MemoryCatalog{Releases: map[string][]CatalogRelease{id: {release}}}, []project.PluginRequirement{{ID: id, Constraint: "^1.0.0"}}, previous)
	if err != nil {
		t.Fatal(err)
	}
	entry := plan.Lock.Plugins[id]
	if !reflect.DeepEqual(entry.Grants, previous.Plugins[id].Grants) || string(entry.Config) != `{"mode":"safe"}` {
		t.Fatalf("preserved review state = %#v", entry)
	}
	if len(plan.Reviews) != 1 || plan.Reviews[0].Change != "changed" {
		t.Fatalf("authority reviews = %#v", plan.Reviews)
	}
}

func TestResolveCatalogGraphPreservesNarrowRequiredGrant(t *testing.T) {
	id := "github.com/acme/imports"
	release := testCatalogRelease(t, id, "1.1.0")
	release.Definition.Authorities = []corewago.AuthorityRequest{{
		Name: corewago.AuthorityHostImportDefine, Mode: corewago.AuthorityRequired,
		Reason: "define reviewed host imports", Scope: corewago.AuthorityScope{Modules: []string{"clock", "random"}},
	}}
	release = resignRelease(t, release)
	previous := project.NewLockDocument()
	previous.Plugins[id] = project.LockEntry{
		RequestedAuthorities: []project.AuthorityRequest{{Name: "host.import.define", Mode: "required", Reason: "old", Scope: project.AuthorityScope{Modules: []string{"clock", "random"}}}},
		Grants:               []project.AuthorityGrant{{Name: "host.import.define", Scope: project.AuthorityScope{Modules: []string{"clock"}}}},
		Config:               json.RawMessage(`{}`),
	}
	plan, err := ResolveCatalogGraph(context.Background(), &MemoryCatalog{Releases: map[string][]CatalogRelease{id: {release}}}, []project.PluginRequirement{{ID: id, Constraint: "^1.0.0"}}, previous)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Lock.Plugins[id].Grants; !reflect.DeepEqual(got, previous.Plugins[id].Grants) {
		t.Fatalf("required grant = %#v, want preserved narrow grant %#v", got, previous.Plugins[id].Grants)
	}
}

func TestHTTPCatalogUsesRepeatedRangesAndStrictMetadata(t *testing.T) {
	newest := testCatalogRelease(t, "github.com/acme/plugin", "1.3.0")
	oldest := testCatalogRelease(t, "github.com/acme/plugin", "1.2.0")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got := request.URL.Query()["range"]; !reflect.DeepEqual(got, []string{">=1.1.0", "^1.0.0"}) {
			t.Errorf("ranges = %v", got)
		}
		offset := request.URL.Query().Get("offset")
		if request.URL.Query().Get("limit") != "256" {
			t.Errorf("limit = %q", request.URL.Query().Get("limit"))
		}
		if offset == "0" {
			_ = json.NewEncoder(response).Encode(map[string]any{"plugins": []CatalogRelease{newest}, "total": 2, "offset": 0, "limit": 256, "nextOffset": 1})
			return
		}
		if offset != "1" {
			t.Errorf("offset = %q", offset)
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"plugins": []CatalogRelease{oldest}, "total": 2, "offset": 1, "limit": 256})
	}))
	defer server.Close()
	got, err := (HTTPCatalog{BaseURL: server.URL, Client: server.Client()}).Candidates(context.Background(), newest.ID, []string{">=1.1.0", "^1.0.0"})
	if err != nil || len(got) != 2 || got[0].DefinitionDigest != newest.DefinitionDigest || got[1].DefinitionDigest != oldest.DefinitionDigest {
		t.Fatalf("Candidates = %#v, %v", got, err)
	}
}

func TestHTTPCatalogRejectsRemotePlaintextBeforeRequest(t *testing.T) {
	_, err := (HTTPCatalog{BaseURL: "http://192.0.2.1"}).Candidates(
		context.Background(), "github.com/acme/plugin", []string{"*"})
	if err == nil || !strings.Contains(err.Error(), "HTTPS is required") {
		t.Fatalf("remote plaintext catalog = %v", err)
	}
}

func TestHTTPCatalogDefaultClientRefusesRedirects(t *testing.T) {
	redirected := make(chan struct{}, 1)
	sink := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected <- struct{}{}
	}))
	defer sink.Close()
	registry := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", sink.URL)
		writer.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer registry.Close()

	_, err := (HTTPCatalog{BaseURL: registry.URL}).Candidates(
		context.Background(), "github.com/acme/plugin", []string{"*"})
	if err == nil {
		t.Fatal("redirecting catalog succeeded")
	}
	select {
	case <-redirected:
		t.Fatal("catalog client followed redirect")
	default:
	}
	if !strings.Contains(err.Error(), "307") {
		t.Fatalf("redirecting catalog error = %v", err)
	}
}

func TestCatalogRejectsUnprefixedSourceVersion(t *testing.T) {
	release := testCatalogRelease(t, "github.com/acme/plugin", "1.2.3")
	release.Source.Version = "1.2.3"
	_, err := (&MemoryCatalog{Releases: map[string][]CatalogRelease{release.ID: {release}}}).Candidates(
		context.Background(), release.ID, []string{"*"},
	)
	if err == nil || !strings.Contains(err.Error(), "v-prefixed") {
		t.Fatalf("unprefixed source version error = %v", err)
	}
}

func testCatalogRelease(t *testing.T, id, version string) CatalogRelease {
	t.Helper()
	release := CatalogRelease{
		ID: id, Version: version,
		Source:   project.PluginSource{Module: id, Version: "v" + strings.TrimPrefix(version, "v"), Checksum: "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
		Provider: project.ProviderSource{ImportPath: id + "/register"},
		Definition: corewago.PluginDefinition{
			ID: id, Name: id, Version: version, Stability: corewago.Experimental,
			Compatibility: corewago.Compatibility{Engines: map[string]string{"wago": "*"}},
			Provenance:    corewago.PluginProvenance{Repository: "https://" + id, License: "MIT"},
		},
		ReleaseFingerprint: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	return resignRelease(t, release)
}

func testSharedSourceRelease(t *testing.T, id, module, version, checksum, fingerprint string) CatalogRelease {
	t.Helper()
	release := testCatalogRelease(t, id, version)
	release.Source.Module = module
	release.Source.Checksum = checksum
	release.Provider.ImportPath = module + "/register"
	release.Definition.Provenance.Repository = "https://" + module
	release.ReleaseFingerprint = fingerprint
	return resignRelease(t, release)
}

func resignRelease(t *testing.T, release CatalogRelease) CatalogRelease {
	t.Helper()
	digest, err := corewago.DefinitionDigest(release.Definition)
	if err != nil {
		t.Fatalf("DefinitionDigest(%s): %v", release.ID, err)
	}
	release.DefinitionDigest = digest
	return release
}
