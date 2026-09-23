//go:build tinygo && wago_minimal

package wago

import "errors"

func checkCompiledAVXRequirements(avx2, avx512 bool) error {
	if avx2 || avx512 {
		return errors.New("wago: plugin AVX code is unavailable in minimal TinyGo")
	}
	return nil
}
