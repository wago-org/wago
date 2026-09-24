//go:build !tinygo || !wago_minimal

package wago

import "fmt"

func checkCompiledAVXRequirements(avx2, avx512 bool) error {
	if avx2 && !avx2HostFeaturesSupported() {
		return fmt.Errorf("wago: compiled module requires AVX2 CPU features unavailable on this host")
	}
	if avx512 && !avx512HostFeaturesSupported() {
		return fmt.Errorf("wago: compiled module requires AVX-512 CPU features unavailable on this host")
	}
	return nil
}
