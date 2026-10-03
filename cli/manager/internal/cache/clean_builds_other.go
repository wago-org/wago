//go:build !linux && !darwin && !windows

package cache

import "errors"

func cleanPluginBuilds(string) (Result, error) {
	return Result{}, errors.New("secure global plugin cache cleanup is unsupported on this platform")
}

func sizePluginBuilds(string) (int64, error) {
	return 0, errors.New("secure global plugin cache sizing is unsupported on this platform")
}
