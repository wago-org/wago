//go:build !linux || !amd64 || !wago_experimental_owned_rw_staging

package wago

func configureOwnedRWStaging(opts *railshotCompileOptions) {}
