//go:build !wago_profile || !amd64

package main

import "fmt"

func captureCommand(input, output string) {
	fail(fmt.Errorf("capture requires an AMD64 build with -tags=wago_profile; comparison is available in ordinary builds"))
}
