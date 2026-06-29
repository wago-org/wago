package gc

import "fmt"

const (
	defaultThroughputHeapBytes  = 16 << 20
	defaultThroughputPageBytes  = 64 << 10
	defaultThroughputClassLimit = 32 << 10
)

func normalizeConfig(cfg Config) (Config, error) {
	if cfg.Policy == PolicyTiny {
		if cfg.Profile != ProfileThroughput && cfg.Profile != ProfileTiny {
			return cfg, fmt.Errorf("gc: unsupported profile %d", cfg.Profile)
		}
		cfg.Profile = ProfileTiny
	}
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
		if cfg.Allocator != AllocatorTinyFixedBlock || cfg.Runtime != RuntimeIncrementalMarkSweep {
			return cfg, fmt.Errorf("gc: profile tiny requires fixed-block allocator and incremental mark/sweep runtime")
		}
		cfg.Policy = PolicyTiny
		return cfg, nil
	}
	if cfg.Allocator != AllocatorPagedSizeClass || cfg.Runtime != RuntimeGenerational {
		return cfg, fmt.Errorf("gc: profile throughput requires paged size-class allocator and generational runtime")
	}
	cfg.Policy = PolicyDefault
	if cfg.ThroughputHeapBytes == 0 {
		cfg.ThroughputHeapBytes = defaultThroughputHeapBytes
	}
	if cfg.ThroughputPageBytes == 0 {
		cfg.ThroughputPageBytes = defaultThroughputPageBytes
	}
	if cfg.ThroughputClassLimit == 0 {
		cfg.ThroughputClassLimit = defaultThroughputClassLimit
	}
	return cfg, nil
}
