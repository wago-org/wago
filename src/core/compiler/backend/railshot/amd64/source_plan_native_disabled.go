//go:build amd64 && (!wago_regalloccheck || tinygo || wago_profile)

package amd64

func checkNativeSourceBefore(*fn, int, byte)     {}
func checkNativeSourceAfter(*fn, int)            {}
func checkNativeSourceGet(*fn, uint32)           {}
func checkNativeSourceSet(*fn, int, bool, *elem) {}

func checkNativeSourceALUBefore(*fn, *elem, *elem, *elem) {}
func checkNativeSourceALUAfter(*fn)                       {}
