//go:build darwin

package watchstart

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

const environment = "WAGO_INTERNAL_WATCH_START_FDS"

func Prepare(command *exec.Cmd) error {
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	readFD := 3 + len(command.ExtraFiles)
	command.ExtraFiles = append(command.ExtraFiles, reader, writer)
	command.Env = append(command.Environ(), fmt.Sprintf("%s=%d,%d", environment, readFD, readFD+1))
	return nil
}

func Started(command *exec.Cmd) error {
	reader, _, ok := barrierFiles(command)
	if !ok {
		return nil
	}
	return reader.Close()
}

func Release(command *exec.Cmd) error {
	_, writer, ok := barrierFiles(command)
	if !ok {
		return nil
	}
	_, err := writer.Write([]byte{1})
	closeErr := writer.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func Abort(command *exec.Cmd) {
	reader, writer, ok := barrierFiles(command)
	if !ok {
		return
	}
	_ = reader.Close()
	_ = writer.Close()
}

func Await() {
	value, ok := os.LookupEnv(environment)
	if !ok {
		return
	}
	_ = os.Unsetenv(environment)
	readValue, writeValue, ok := strings.Cut(value, ",")
	if !ok {
		return
	}
	readFD, readErr := strconv.Atoi(readValue)
	writeFD, writeErr := strconv.Atoi(writeValue)
	if readErr != nil || writeErr != nil || readFD < 3 || writeFD < 3 || readFD == writeFD {
		return
	}
	if !pipePair(readFD, writeFD) {
		return
	}
	reader := os.NewFile(uintptr(readFD), "wago-watch-start-reader")
	writer := os.NewFile(uintptr(writeFD), "wago-watch-start-writer")
	_ = writer.Close()
	var release [1]byte
	_, err := io.ReadFull(reader, release[:])
	_ = reader.Close()
	if err != nil || release[0] != 1 {
		os.Exit(1)
	}
}

func barrierFiles(command *exec.Cmd) (*os.File, *os.File, bool) {
	if len(command.ExtraFiles) < 2 {
		return nil, nil, false
	}
	return command.ExtraFiles[len(command.ExtraFiles)-2], command.ExtraFiles[len(command.ExtraFiles)-1], true
}

func pipePair(readFD, writeFD int) bool {
	var reader, writer syscall.Stat_t
	if syscall.Fstat(readFD, &reader) != nil || syscall.Fstat(writeFD, &writer) != nil {
		return false
	}
	return reader.Mode&syscall.S_IFMT == syscall.S_IFIFO &&
		writer.Mode&syscall.S_IFMT == syscall.S_IFIFO &&
		reader.Dev == writer.Dev && reader.Ino == writer.Ino
}
