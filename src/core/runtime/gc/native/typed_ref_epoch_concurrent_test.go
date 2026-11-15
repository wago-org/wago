package gc

import (
	"sync"
	"testing"
)

func TestNativeTypeEpochConcurrentInvalidation(t *testing.T) {
	c, err := NewCollector(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var done sync.WaitGroup
	for range 8 {
		done.Add(1)
		go func() {
			defer done.Done()
			for range 1000 {
				c.AdvanceNativeTypeEpoch()
			}
		}()
	}
	done.Wait()
	if got := c.NativeTypeEpoch(); got != 8001 {
		t.Fatalf("invalidation updates lost: epoch%d want8001", got)
	}
}
