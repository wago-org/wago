//go:build !wago_profile || !amd64

package main

import "fmt"

func captureFile(input, output string) error {
	return fmt.Errorf("capture requires an AMD64 build with -tags=wago_profile; comparison is available in ordinary builds")
}
