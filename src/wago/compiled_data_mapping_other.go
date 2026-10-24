//go:build !linux

package wago

func mapCompactActiveData(*Compiled, int) ([]byte, error) { return nil, nil }
func unmapCompactData([]byte) error                       { return nil }
