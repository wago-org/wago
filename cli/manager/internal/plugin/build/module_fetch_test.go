package build

import (
	"errors"
	"testing"
)

func TestIsNotFound(t *testing.T) {
	err := &FetchError{
		Err:    errors.New("exit status 1"),
		Output: "remote: Repository not found.\nfatal: repository not found",
	}
	if !IsNotFound(err) {
		t.Fatal("repository-not-found response was not classified")
	}
	if IsNotFound(errors.New("network unavailable")) {
		t.Fatal("generic error was classified as not found")
	}
}
