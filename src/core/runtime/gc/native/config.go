package gc

import "fmt"

const (
	defaultThroughputHeapBytes  = 16 << 20
	defaultThroughputPageBytes  = 64 << 10
	defaultThroughputClassLimit = 32 << 10
	defaultThroughputCardBytes  = 128
	defaultTenuringThreshold    = 2
)

// ValidateConfig rejects unsupported collector-profile combinations without
// allocating heap backing storage.
func ValidateConfig(cfg Config) error {
	_, err := normalizeConfig(cfg)
	return err
}

func normalizeConfig(cfg Config) (Config, error) {
	switch cfg.Profile {
	case ProfileThroughput:
		if cfg.Allocator == 0 && cfg.Runtime == 0 {
			cfg.Allocator = AllocatorPagedSizeClass
			cfg.Runtime = RuntimeGenerational
		}
	case ProfileTiny:
		if cfg.Allocator == 0 && cfg.Runtime == 0 {
			cfg.Allocator = AllocatorTinyFixedBlock
			cfg.Runtime = RuntimeIncrementalMarkSweep
		}
	default:
		return cfg, fmt.Errorf("gc: unsupported profile %d", cfg.Profile)
	}
	if cfg.Profile == ProfileTiny {
		if cfg.SurvivorBytes != 0 || cfg.MinorPauseTargetMicros != 0 {
			return cfg, fmt.Errorf("gc: survivor policy requires the throughput profile")
		}
		if cfg.DisableCollection {
			return cfg, fmt.Errorf("gc: collection-disabled mode requires the throughput profile")
		}
		if cfg.Allocator != AllocatorTinyFixedBlock || cfg.Runtime != RuntimeIncrementalMarkSweep {
			return cfg, fmt.Errorf("gc: profile tiny requires fixed-block allocator and incremental mark/sweep runtime")
		}
		return cfg, nil
	}
	if cfg.DisableMovingNursery && (cfg.SurvivorBytes != 0 || cfg.MinorPauseTargetMicros != 0) {
		return cfg, fmt.Errorf("gc: disabled moving nursery cannot configure survivor policy")
	}
	if cfg.Allocator != AllocatorPagedSizeClass || cfg.Runtime != RuntimeGenerational {
		return cfg, fmt.Errorf("gc: profile throughput requires paged size-class allocator and generational runtime")
	}
	if cfg.ThroughputHeapBytes == 0 {
		cfg.ThroughputHeapBytes = defaultThroughputHeapBytes
	}
	if cfg.ThroughputPageBytes == 0 {
		cfg.ThroughputPageBytes = defaultThroughputPageBytes
	}
	if cfg.ThroughputClassLimit == 0 {
		cfg.ThroughputClassLimit = defaultThroughputClassLimit
	}
	if cfg.StressNurseryBytes != 0 {
		cfg.NurseryBytes = cfg.StressNurseryBytes
	}
	if cfg.NurseryBytes == 0 {
		cfg.NurseryBytes = defaultNursery
	}
	if !cfg.DisableMovingNursery && cfg.SurvivorBytes == 0 {
		cfg.SurvivorBytes = align(cfg.NurseryBytes/2, 16)
	}
	if cfg.SurvivorBytes > ^uint32(0)-15 {
		return cfg, fmt.Errorf("gc: survivor space exceeds addressable backing")
	}
	cfg.SurvivorBytes = align(cfg.SurvivorBytes, 16)
	survivorBase := align(cfg.NurseryBytes, 16)
	if survivorBase < cfg.NurseryBytes || cfg.SurvivorBytes > (^uint32(0)-survivorBase)/2 {
		return cfg, fmt.Errorf("gc: nursery and survivor spaces exceed addressable backing")
	}
	return cfg, nil
}
