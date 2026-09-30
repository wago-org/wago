//go:build !wago_profile

package wago

// Ordinary compiled modules carry no profiling state or thunk directory.
type profileCacheState struct{}

func (*compiledCodeCache) loadProfile() *compiledProfile                             { return nil }
func (*compiledCodeCache) storeProfile(*compiledProfile)                             {}
func (*compiledCodeCache) attachProfile(*compiledProfile, []byte)                    {}
func (*compiledCodeCache) registerProfileThunk(uintptr, []byte, []CodeProfileRegion) {}
func (*compiledCodeCache) retireProfileImage(uintptr)                                {}
