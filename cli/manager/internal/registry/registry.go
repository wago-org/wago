// Package registry owns authentication, package resolution, publishing, and
// registry credentials for the Wago manager.
package registry

import "context"

type LoginRequest struct {
	Link, Code, WithToken bool
	Token                 string
}

type PublishRequest struct {
	Manifest, Commit, Notes, Category, Tags string
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

func LoginContext(ctx context.Context, request LoginRequest) { registryLoginContext(ctx, request) }
func LogoutContext(ctx context.Context)                      { registryLogoutContext(ctx) }
func WhoamiContext(ctx context.Context)                      { registryWhoamiContext(ctx) }
func PublishContext(ctx context.Context, request PublishRequest) {
	registryPublishContext(ctx, request)
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
