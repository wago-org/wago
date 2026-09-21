//go:build !arm64 || (!darwin && !linux)

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "arm64cost: native ARM64 calibration requires darwin/arm64 or linux/arm64")
	os.Exit(2)
}
