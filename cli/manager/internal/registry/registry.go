// Package registry owns authentication, package resolution, publishing, and
// registry credentials for the Wago manager.
package registry

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

func Login(request LoginRequest)                { registryLogin(request) }
func Logout()                                   { registryLogout() }
func Whoami()                                   { registryWhoami() }
func Publish(request PublishRequest)            { registryPublish(request) }
func Unpublish(request UnpublishRequest)        { registryUnpublish(request) }
func Deprecate(request DeprecateRequest)        { registryDeprecate(request) }
func ResolveModule(name string) (string, error) { return resolveRegistryModule(name) }
func RecordInstall(module, version string)      { recordRegistryInstall(module, version) }
