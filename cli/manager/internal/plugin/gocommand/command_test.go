package gocommand

import "testing"

func TestLimitedStderrRetainsHeadAndTail(t *testing.T) {
	stderr := &limitedStderr{limit: 4}
	for _, value := range []byte("abcdefghijk") {
		if _, err := stderr.Write([]byte{value}); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := string(stderr.Bytes()), "abcd\n... omitting 3 bytes ...\nhijk"; got != want {
		t.Fatalf("bounded stderr = %q, want %q", got, want)
	}
}

func TestLimitedStderrOversizedChunkWrapsTail(t *testing.T) {
	stderr := &limitedStderr{limit: 4}
	for _, chunk := range []string{"abcd", "ef", "ghijklmnop"} {
		if _, err := stderr.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := string(stderr.Bytes()), "abcd\n... omitting 8 bytes ...\nmnop"; got != want {
		t.Fatalf("bounded stderr = %q, want %q", got, want)
	}
}
