//go:build arm64 && (!wago_regalloccheck || tinygo || wago_profile)

package arm64

func checkSourceBegin(*fn, bool)    {}
func checkSourceFinishEmission(*fn) {}
func checkSourceVerify(*fn)         {}
func checkSourceClose(*fn)          {}
