// Package modulefile bounds CLI reads of untrusted Wasm and artifact inputs.
package modulefile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/wago-org/wago"
)

// MaxBytes is the largest module input accepted by CLI commands.
const MaxBytes int64 = 256 << 20

// MaxArtifactBytes matches the public artifact decoder plus framing overhead.
const MaxArtifactBytes int64 = (1 << 30) + (256 << 20) + 64

// Input is one bounded module input. Compiled artifacts remain streaming;
// source modules can be materialized exactly once for compilation.
type Input struct {
	path     string
	file     *os.File
	reader   *io.LimitedReader
	size     int64
	regular  bool
	artifact bool
	limit    int64
}

// Read reads path without accepting more than MaxBytes.
func Read(path string) ([]byte, error) {
	input, err := open(path, false)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	return input.ReadSource()
}

// OpenSourceOrArtifact inspects a run-command input without materializing a
// compiled artifact. The caller must close the returned input.
func OpenSourceOrArtifact(path string) (*Input, error) {
	return open(path, true)
}

func open(path string, allowArtifact bool) (*Input, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	buffered := bufio.NewReaderSize(file, 5)
	limit := MaxBytes
	artifact := false
	if allowArtifact {
		prefix, peekErr := buffered.Peek(5)
		if peekErr != nil && !errors.Is(peekErr, io.EOF) {
			file.Close()
			return nil, peekErr
		}
		artifact = wago.IsCompiled(prefix)
		limit = inputLimit(prefix)
	}
	regular := info.Mode().IsRegular()
	if regular && info.Size() > limit {
		file.Close()
		return nil, fmt.Errorf("module %q is %d bytes; CLI limit is %d bytes", path, info.Size(), limit)
	}
	return &Input{
		path: path, file: file, reader: &io.LimitedReader{R: buffered, N: limit + 1},
		size: info.Size(), regular: regular, artifact: artifact, limit: limit,
	}, nil
}

// Read implements io.Reader for streaming artifact decoding.
func (input *Input) Read(p []byte) (int, error) {
	if input == nil || input.reader == nil {
		return 0, io.EOF
	}
	return input.reader.Read(p)
}

// Close closes the underlying module input.
func (input *Input) Close() error {
	if input == nil || input.file == nil {
		return nil
	}
	err := input.file.Close()
	input.file = nil
	return err
}

// IsArtifact reports whether the input carries Wago compiled-artifact magic.
func (input *Input) IsArtifact() bool { return input != nil && input.artifact }

// Size reports the stable pre-read size of a regular input.
func (input *Input) Size() (int64, bool) {
	if input == nil || !input.regular {
		return 0, false
	}
	return input.size, true
}

// ReadSource materializes a bounded source module with exact slice capacity.
func (input *Input) ReadSource() ([]byte, error) {
	if input == nil {
		return nil, fmt.Errorf("module input is nil")
	}
	if input.artifact {
		return nil, fmt.Errorf("compiled artifact must be decoded as a stream")
	}
	if !input.regular {
		return readStream(input.path, input, input.limit)
	}
	// Reserve one sentinel byte so growth after Stat is detected without
	// io.ReadAll's geometric over-allocation.
	data := make([]byte, int(input.size+1))
	n, err := io.ReadFull(input, data)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	if int64(n) > input.limit {
		return nil, fmt.Errorf("module %q exceeds CLI limit of %d bytes", input.path, input.limit)
	}
	if int64(n) > input.size {
		return nil, fmt.Errorf("module %q changed size while being read", input.path)
	}
	return data[:n:n], nil
}

func inputLimit(prefix []byte) int64 {
	if wago.IsCompiled(prefix) {
		return MaxArtifactBytes
	}
	return MaxBytes
}

// readStream spools pipes and other unknown-length inputs through a bounded
// fixed-size buffer. This avoids reserving the entire input limit before the
// stream has produced any bytes while still returning one exact-sized slice.
func readStream(path string, source io.Reader, limit int64) ([]byte, error) {
	temporary, err := os.CreateTemp("", "wago-module-*")
	if err != nil {
		return nil, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	defer temporary.Close()

	buffer := make([]byte, 64<<10)
	n, err := io.CopyBuffer(temporary, io.LimitReader(source, limit+1), buffer)
	if err != nil {
		return nil, err
	}
	if n > limit {
		return nil, fmt.Errorf("module %q exceeds CLI limit of %d bytes", path, limit)
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data := make([]byte, int(n))
	read, err := io.ReadFull(temporary, data)
	if err != nil {
		return nil, err
	}
	return data[:read:read], nil
}
