//go:build !darwin || !cgo

package main

func setBenchmarkQoS() (int, uint32) { return -1, 0 }
