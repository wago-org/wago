//go:build !darwin && !linux

package main

import "os"

func peakRSS(*os.ProcessState) uint64 { return 0 }
