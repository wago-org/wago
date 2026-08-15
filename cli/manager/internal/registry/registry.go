// Package registry owns authentication, package resolution, publishing, and
// registry credentials for the Wago manager.
package registry

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago/cli/internal/project"
)

type LoginRequest struct {
	Link, Code, WithToken bool
	Token                 string
}

type PublishRequest struct {
	Manifest, Notes string
}

type CatalogRequest struct {
	Manifest string
	Check    bool
}

type UnpublishRequest struct {
	Target string
	Yes    bool
}

type DeprecateRequest struct {
	Target, Message string
	Undo            bool
}

func Login(request LoginRequest)         { registryLogin(request) }
func Logout()                            { registryLogout() }
func Whoami()                            { registryWhoami() }
func Publish(request PublishRequest)     { registryPublish(request) }
func Catalog(request CatalogRequest)     { registryCatalogContext(context.Background(), request) }
func Unpublish(request UnpublishRequest) { registryUnpublish(request) }
func Deprecate(request DeprecateRequest) { registryDeprecate(request) }
func ResolveModule(name string) (string, error) {
	return resolveRegistryModule(name)
}
func RecordInstall(module, version string) { recordRegistryInstall(module, version) }
func RecordInstallContext(ctx context.Context, module, version string) {
	recordRegistryInstallContext(ctx, module, version)
}
func Closest(module string) string { return closestModule(module) }

// ValidateCatalogPlan performs the non-writing work required by a catalog dry
// run, including loading the provider package and checking a committed catalog
// when requested.
func ValidateCatalogPlan(ctx context.Context, request CatalogRequest) (string, error) {
	path := request.Manifest
	if path == "" {
		path = project.File
	}
	_, metadata, err := readPublishManifest(path)
	if err != nil {
		return path, err
	}
	catalog, err := buildProviderCatalog(ctx, path, metadata)
	if err != nil {
		return path, err
	}
	if request.Check {
		err = checkProviderCatalogCurrent(catalog)
	}
	return path, err
}

// ValidatePublishPlan performs the local, non-publishing checks required by a
// publish dry run. Authentication, tag creation, source download, and upload
// remain execution-time work because they require remote state or mutations.
func ValidatePublishPlan(ctx context.Context, path string) (string, error) {
	if path == "" {
		path = project.File
	}
	_, metadata, err := readPublishManifest(path)
	if err != nil {
		return path, err
	}
	version := strings.TrimSpace(metadata.Version)
	if version == "" {
		version = strings.TrimSpace(gitOutputAt(filepath.Dir(path), "describe", "--tags", "--abbrev=0"))
	}
	if version == "" {
		return path, errors.New("no version; set package.version or tag the repository")
	}
	version = canonicalGoVersion(version)
	catalog, err := buildProviderCatalog(ctx, path, metadata)
	if err != nil {
		return path, fmt.Errorf("local provider catalog: %w", err)
	}
	if strings.TrimPrefix(catalog.version, "v") != strings.TrimPrefix(version, "v") {
		return path, fmt.Errorf("local provider version %s does not match release %s", catalog.version, version)
	}
	return path, checkProviderCatalogCurrent(catalog)
}

func LoginContext(ctx context.Context, request LoginRequest) { registryLoginContext(ctx, request) }
func LogoutContext(ctx context.Context)                      { registryLogoutContext(ctx) }
func WhoamiContext(ctx context.Context)                      { registryWhoamiContext(ctx) }
func PublishContext(ctx context.Context, request PublishRequest) {
	registryPublishContext(ctx, request)
}
func CatalogContext(ctx context.Context, request CatalogRequest) {
	registryCatalogContext(ctx, request)
}
func UnpublishContext(ctx context.Context, request UnpublishRequest) {
	registryUnpublishContext(ctx, request)
}
func DeprecateContext(ctx context.Context, request DeprecateRequest) {
	registryDeprecateContext(ctx, request)
}
func ResolveModuleContext(ctx context.Context, name string) (string, error) {
	return resolveRegistryModuleContext(ctx, name)
}
func ClosestContext(ctx context.Context, module string) string {
	return closestModuleContext(ctx, module)
}
