//go:build ((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package runtime

import (
	"errors"
	"testing"
)

func TestCodePublicationFailureReturnsCleanupOwnership(t *testing.T) {
	for _, phase := range []string{"protect", "flush", "register"} {
		for _, failRollback := range []bool{false, true} {
			t.Run(phase+map[bool]string{false: "/released", true: "/retained"}[failRollback], func(t *testing.T) {
				primary, cleanupFailure := errors.New("publication failure"), errors.New("rollback failure")
				var allocation []byte
				unmaps, registrations := 0, 0
				unmap := func(mem []byte) error {
					unmaps++
					if failRollback && unmaps == 1 {
						return cleanupFailure
					}
					return munmap(mem)
				}
				mapExec := func(code []byte) ([]byte, error) {
					return mmapExecWith(code, func(n int) ([]byte, error) {
						var err error
						allocation, err = mmapCodeRW(n)
						return allocation, err
					}, func(mem []byte) error {
						if phase == "protect" {
							return primary
						}
						if err := protectCodeRX(mem); err != nil {
							return err
						}
						if phase == "flush" {
							return primary // failure after RX, like a cache syscall error
						}
						return nil
					}, unmap)
				}
				mem, entry, err := mapCodeWith([]byte{0, 0, 0, 0}, mapExec, func([]byte) error {
					registrations++
					return primary
				}, unmap)
				// Keep the original handle until the test has established release.
				t.Cleanup(func() {
					if allocation != nil {
						if err := munmap(allocation); err != nil {
							t.Error(err)
						}
					}
				})
				if entry != 0 || !errors.Is(err, primary) || unmaps != 1 || errors.Is(err, cleanupFailure) != failRollback {
					t.Fatalf("entry=%x err=%v unmaps=%d", entry, err, unmaps)
				}
				if registrations != map[bool]int{false: 0, true: 1}[phase == "register"] {
					t.Fatal("registration occurred before protection/cache success")
				}
				if failRollback {
					if len(mem) != len(allocation) || slicePtr(mem) != slicePtr(allocation) {
						t.Fatal("failed rollback lost its cleanup mapping")
					}
					if err := unmap(mem); err != nil || unmaps != 2 {
						t.Fatal("cleanup retry", err, unmaps)
					}
				} else if mem != nil {
					t.Fatal("successful rollback retained a released mapping")
				}
				allocation = nil
			})
		}
	}
}
