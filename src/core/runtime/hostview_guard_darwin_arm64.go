//go:build wago_guardpage && darwin && arm64

package runtime

import "syscall"

func growGuardedHostView(j *JobMemory, logicalBytes int) error {
	committed := len(j.mem) - j.linOff
	if logicalBytes <= committed {
		return nil
	}
	end := j.linOff + logicalBytes
	full := j.mem[:end]
	if err := syscall.Mprotect(full[j.linOff+committed:end], syscall.PROT_READ|syscall.PROT_WRITE); err != nil {
		return err
	}
	j.mem = full
	return nil
}
