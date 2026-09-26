package wago

import (
	"bytes"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"sync"
)

var amd64CPUCache struct {
	once     sync.Once
	features shared.AMD64Features
	ok       bool
}

func cachedAMD64CPUFeatures() (shared.AMD64Features, bool) {
	amd64CPUCache.once.Do(func() { amd64CPUCache.features, amd64CPUCache.ok = architectureAMD64CPUFeatures() })
	return amd64CPUCache.features, amd64CPUCache.ok
}

func amd64CPUIDFeatures(ecx1, ebx7, extECX, xcr0 uint32) (f shared.AMD64Features) {
	if ecx1&(1<<9) != 0 {
		f |= shared.AMD64SSSE3
	}
	if ecx1&(1<<19) != 0 {
		f |= shared.AMD64SSE41
	}
	if ecx1&(1<<20) != 0 {
		f |= shared.AMD64SSE42
	}
	if ecx1&(1<<23) != 0 {
		f |= shared.AMD64POPCNT
	}
	if ebx7&(1<<3) != 0 {
		f |= shared.AMD64BMI1
	}
	if ebx7&(1<<8) != 0 {
		f |= shared.AMD64BMI2
	}
	if extECX&(1<<5) != 0 {
		f |= shared.AMD64LZCNT
	}
	const avxState = uint32(1<<26 | 1<<27 | 1<<28)
	if ecx1&avxState == avxState && xcr0&6 == 6 {
		f |= shared.AMD64AVX
		if ecx1&(1<<12) != 0 {
			f |= shared.AMD64FMA
		}
		if ebx7&(1<<5) != 0 {
			f |= shared.AMD64AVX2
		}
		const avx512 = uint32(1<<16 | 1<<17 | 1<<30 | 1<<31)
		if f.Has(shared.AMD64AVX2) && ebx7&avx512 == avx512 && xcr0&0xe6 == 0xe6 {
			f |= shared.AMD64AVX512
		}
	}
	return
}

// amd64LinuxCPUFeatures intersects every logical CPU's flags. Linux exposes
// AVX-family flags only after enabling the corresponding extended OS state.
// Missing/unreadable flags fail closed, including for baseline-only execution.
func amd64LinuxCPUFeatures(data []byte) (shared.AMD64Features, bool) {
	features := shared.AMD64KnownFeatures
	seen := false
	for len(data) > 0 {
		line := data
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			data = nil
		}
		colon := bytes.IndexByte(line, ':')
		if colon < 0 || !bytes.Equal(bytes.TrimSpace(line[:colon]), []byte("flags")) {
			continue
		}
		flags := line[colon+1:]
		if !cpuFlagPresent(flags, "sse2") {
			return 0, false
		}
		var f shared.AMD64Features
		for _, entry := range [...]struct {
			flag    string
			feature shared.AMD64Features
		}{
			{"ssse3", shared.AMD64SSSE3}, {"sse4_1", shared.AMD64SSE41}, {"sse4_2", shared.AMD64SSE42},
			{"avx", shared.AMD64AVX}, {"avx2", shared.AMD64AVX2}, {"bmi1", shared.AMD64BMI1}, {"bmi2", shared.AMD64BMI2},
			{"abm", shared.AMD64LZCNT}, {"popcnt", shared.AMD64POPCNT}, {"fma", shared.AMD64FMA},
		} {
			if cpuFlagPresent(flags, entry.flag) {
				f |= entry.feature
			}
		}
		if !f.Has(shared.AMD64AVX) {
			f &^= shared.AMD64AVX2 | shared.AMD64FMA
		}
		if f.Has(shared.AMD64AVX|shared.AMD64AVX2) && cpuFlagPresent(flags, "avx512f") && cpuFlagPresent(flags, "avx512dq") && cpuFlagPresent(flags, "avx512bw") && cpuFlagPresent(flags, "avx512vl") {
			f |= shared.AMD64AVX512
		}
		features &= f
		seen = true
	}
	return features, seen
}

func checkAMD64Requirements(required, available shared.AMD64Features, detected bool) error {
	if !detected {
		return fmt.Errorf("wago: AMD64 CPU capability detection failed")
	}
	if missing := required &^ available; missing != 0 {
		return fmt.Errorf("wago: compiled module requires unavailable AMD64 CPU features %#x", missing)
	}
	return nil
}
