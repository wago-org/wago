//go:build !tinygo

package compiler

import (
	"runtime"

	"golang.org/x/sys/cpu"
)

func applyHostTargetFeatures(target *Target) {
	switch runtime.GOARCH {
	case "amd64":
		target.setFeature(TargetFeatureAMD64BMI2, cpu.X86.HasBMI2)
		target.setFeature(TargetFeatureAMD64AVX2, cpu.X86.HasAVX2)
		target.setFeature(TargetFeatureAMD64AVX512, cpu.X86.HasAVX512)
		target.setFeature(TargetFeatureAMD64ERMS, cpu.X86.HasERMS)
		target.setFeature(TargetFeatureAMD64FMA, cpu.X86.HasFMA)
	case "arm64":
		target.setFeature(TargetFeatureARM64AES, cpu.ARM64.HasAES)
		target.setFeature(TargetFeatureARM64ATOMICS, cpu.ARM64.HasATOMICS)
		target.setFeature(TargetFeatureARM64CRC32, cpu.ARM64.HasCRC32)
		target.setFeature(TargetFeatureARM64SVE, cpu.ARM64.HasSVE)
		target.setFeature(TargetFeatureARM64SVE2, cpu.ARM64.HasSVE2)
		target.setFeature(TargetFeatureARM64MOPS, hostHasARM64MOPS())
	}
}
