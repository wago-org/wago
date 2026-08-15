//go:build windows && !wago_lean

package run

import "os"

func watchedChildLaunch() (string, []string, error) {
	return os.Args[0], nil, nil
}
