//go:build !linux || !amd64 || tinygo || wago_target_tinygo

package gc

func (*Collector) initOwnedNursery(uint32) bool { return false }
func (*Collector) releaseOwnedNursery()         {}
