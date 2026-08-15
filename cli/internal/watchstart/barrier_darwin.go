//go:build darwin

package watchstart

import (
	"fmt"
	"os"
	"os/exec"
)

func Prepare(command *exec.Cmd) error {
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	readFD := 3 + len(command.ExtraFiles)
	command.ExtraFiles = append(command.ExtraFiles, reader, writer)
	originalPath := command.Path
	originalArguments := append([]string(nil), command.Args[1:]...)
	script := fmt.Sprintf("exec %d>&-; IFS= read -r release <&%d; exec %d<&-; [ \"$release\" = x ] || exit 1; exec \"$@\"", readFD+1, readFD, readFD)
	command.Path = "/bin/sh"
	command.Args = append([]string{"sh", "-c", script, "wago-watch-start", originalPath}, originalArguments...)
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
	_, err := writer.Write([]byte{'x', '\n'})
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

func barrierFiles(command *exec.Cmd) (*os.File, *os.File, bool) {
	if len(command.ExtraFiles) < 2 {
		return nil, nil, false
	}
	return command.ExtraFiles[len(command.ExtraFiles)-2], command.ExtraFiles[len(command.ExtraFiles)-1], true
}
