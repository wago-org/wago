//go:build !compiler_profile

package main

func startProfile(path string) func() {
	if path != "" {
		panic("build with -tags=compiler_profile for diagnostic profiles")
	}
	return func() {}
}
