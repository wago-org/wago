package plugin

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"
	corewago "github.com/wago-org/wago/src/wago"
)

func TestRemovalPreservesDeniedAuthority(t *testing.T) {
	automation.Configure(automation.Options{NoInput: true})
	t.Cleanup(automation.Reset)
	for _, mode := range []corewago.AuthorityMode{corewago.AuthorityOptional, corewago.AuthorityRequired} {
		t.Run(string(mode), func(t *testing.T) {
			retained, previous := testDeniedAuthorityResolution(t, mode)
			removed := testCatalogRelease(t, "github.com/acme/removed", "1.0.0")
			previous.Plugins[removed.ID], _ = lockEntryFromRelease(removed, true, project.LockEntry{})
			manifest := map[string]any{"plugins": map[string]any{retained.ID: "*", removed.ID: "*"}}
			dir := t.TempDir()
			if err := project.WithMutation(context.Background(), dir, func(mutation *project.Mutation) error {
				return mutation.PublishMetadata(manifest, previous)
			}); err != nil {
				t.Fatal(err)
			}

			// Exercise the removal resolution and review using the saved decision.
			err := project.WithMutation(context.Background(), dir, func(mutation *project.Mutation) error {
				manifest, err := mutation.ReadManifest()
				if err != nil {
					return err
				}
				previous, err := mutation.ReadLock()
				if err != nil {
					return err
				}
				if _, err := project.DeleteRequirement(manifest, removed.ID); err != nil {
					return err
				}
				roots, err := project.RequirementsFromManifest(manifest)
				if err != nil {
					return err
				}
				catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{retained.ID: {retained}}}
				plan, err := ResolveCatalogGraph(context.Background(), catalog, roots, previous)
				if err != nil {
					return err
				}
				reviewed, err := reviewRemovalResolution(plan, pkgOpts{})
				if err != nil {
					return err
				}
				if wantWarning := mode == corewago.AuthorityRequired; (len(reviewed.Warnings) != 0) != wantWarning {
					t.Errorf("warnings = %v, want warning = %v", reviewed.Warnings, wantWarning)
				}
				return mutation.PublishMetadata(manifest, reviewed.Lock)
			})
			if err != nil {
				t.Fatalf("remove unrelated plugin with saved %s denial: %v", mode, err)
			}
			lock, err := project.ReadLock(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(lock.Plugins) != 1 || !reflect.DeepEqual(lock.Plugins[retained.ID], previous.Plugins[retained.ID]) {
				t.Fatalf("removal changed retained plugin policy: %#v", lock.Plugins)
			}
			if _, err := project.PluginSelections(dir); err != nil {
				t.Fatalf("published removal cannot be loaded: %v", err)
			}
			granted, err := editAuthorityGrants(lock, retained.ID, nil, true, false, nil)
			if err != nil || len(granted.Plugins[retained.ID].Grants) != 1 {
				t.Fatalf("explicit grant cannot replace saved denial: %#v, %v", granted, err)
			}
		})
	}
}

func TestResolutionPreservesDenialWhenReviewingAnotherPlugin(t *testing.T) {
	automation.Configure(automation.Options{NoInput: true})
	t.Cleanup(automation.Reset)
	for _, mode := range []corewago.AuthorityMode{corewago.AuthorityOptional, corewago.AuthorityRequired} {
		t.Run(string(mode), func(t *testing.T) {
			retained, previous := testDeniedAuthorityResolution(t, mode)
			// Upgrading a release without changing its request must also retain denial.
			retained.Version, retained.Source.Version, retained.Definition.Version = "1.1.0", "v1.1.0", "1.1.0"
			retained = resignRelease(t, retained)
			added := testCatalogRelease(t, "github.com/acme/added", "1.0.0")
			added.Definition.Authorities = []corewago.AuthorityRequest{{
				Name: corewago.AuthorityHostImportDefine, Mode: corewago.AuthorityOptional, Reason: "provide random imports",
				Scope: corewago.AuthorityScope{Modules: []string{"random"}},
			}}
			added = resignRelease(t, added)
			catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{retained.ID: {retained}, added.ID: {added}}}
			roots := []project.PluginRequirement{{ID: retained.ID, Constraint: "^1.0.0"}, {ID: added.ID, Constraint: "*"}}
			plan, err := ResolveCatalogGraph(context.Background(), catalog, roots, previous)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Reviews) != 1 || plan.Reviews[0].PluginID != added.ID {
				t.Errorf("reviews = %#v, want only newly added plugin", plan.Reviews)
			}
			reviewed, err := reviewResolvedPluginPlan(plan, pkgOpts{grantAll: true})
			if err != nil {
				t.Fatal(err)
			}
			if grants := reviewed.Lock.Plugins[retained.ID].Grants; len(grants) != 0 {
				t.Errorf("review of another plugin restored denied grants: %#v", grants)
			}
			if grants := reviewed.Lock.Plugins[added.ID].Grants; len(grants) != 1 {
				t.Errorf("new plugin grants = %#v", grants)
			}
			if err := project.ValidateLockedResolution(roots, reviewed.Lock); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeniedAuthorityChangesStillRequireReview(t *testing.T) {
	automation.Configure(automation.Options{NoInput: true})
	t.Cleanup(automation.Reset)
	for _, change := range []string{"reason", "mode", "scope"} {
		t.Run(change, func(t *testing.T) {
			release, previous := testDeniedAuthorityResolution(t, corewago.AuthorityOptional)
			request := &release.Definition.Authorities[0]
			switch change {
			case "reason":
				request.Reason = "provide additional imports"
			case "mode":
				request.Mode = corewago.AuthorityRequired
			case "scope":
				request.Scope.Modules = []string{"clock", "random"}
			}
			release = resignRelease(t, release)
			catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{release.ID: {release}}}
			plan, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: release.ID, Constraint: "*"}}, previous)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Reviews) != 1 || plan.Reviews[0].Change != "changed" {
				t.Fatalf("changed request reviews = %#v", plan.Reviews)
			}
			if _, err := reviewResolvedPluginPlan(plan, pkgOpts{}); err == nil || !strings.Contains(err.Error(), "authority review") {
				t.Fatalf("changed request accepted without review: %v", err)
			}
			if _, err := reviewRemovalResolution(plan, pkgOpts{}); err == nil || !strings.Contains(err.Error(), "removal changes authority requests") {
				t.Fatalf("removal accepted changed request: %v", err)
			}
		})
	}
}

func testDeniedAuthorityResolution(t *testing.T, mode corewago.AuthorityMode) (CatalogRelease, project.LockDocument) {
	t.Helper()
	release := testCatalogRelease(t, "github.com/acme/retained", "1.0.0")
	release.Definition.Authorities = []corewago.AuthorityRequest{{
		Name: corewago.AuthorityHostImportDefine, Mode: mode, Reason: "provide clock imports",
		Scope: corewago.AuthorityScope{Modules: []string{"clock"}},
	}}
	release = resignRelease(t, release)
	catalog := &MemoryCatalog{Releases: map[string][]CatalogRelease{release.ID: {release}}}
	plan, err := ResolveCatalogGraph(context.Background(), catalog, []project.PluginRequirement{{ID: release.ID, Constraint: "*"}}, project.NewLockDocument())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Reviews) != 1 || plan.Reviews[0].Change != "new" {
		t.Fatalf("initial reviews = %#v", plan.Reviews)
	}
	reviewed, err := reviewResolvedPluginPlan(plan, pkgOpts{denyAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewed.Lock.Plugins[release.ID].Grants) != 0 {
		t.Fatal("initial denial did not clear grants")
	}
	return release, reviewed.Lock
}
