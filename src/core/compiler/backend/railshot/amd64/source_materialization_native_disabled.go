//go:build amd64 && wago_regalloccheck && (tinygo || wago_profile)

package amd64

func checkNativeSourceProducerBefore(*fn, *elem) {}
