package namecheck

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"
)

func TestNameChecksMatchExistingRegex(t *testing.T) {
	checks := []struct {
		name, pattern string
		check         func(string) bool
	}{
		{"canonical", `^(?:[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?\.)+[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:/[A-Za-z0-9](?:[A-Za-z0-9._~-]*[A-Za-z0-9])?)+$`, CanonicalPath},
		{"slug", `^[a-z0-9][a-z0-9._-]*$`, Slug},
		{"platform", `^[a-z0-9]+/[a-z0-9]+$`, Platform},
		{"github", `^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`, GitHubUser},
	}
	seeds := []string{"", "a", "A0", "a-b", "a._-", "linux/arm64", "a.b/c", "example.com/a-b/c.d_e~f", "A-B.c-D/E-F", "a.b/c/", "a..b/c", "a.b//c", "a.b/.c", "a.b/c.", "a.b/c\n", "a.b/é", "a.b/c\x00"}
	for _, n := range []int{0, 1, 2, 37, 38, 39, 40, 64, 65, 299, 300, 301} {
		seeds = append(seeds, strings.Repeat("a", n), "a.b/"+strings.Repeat("a", n))
	}
	for _, test := range checks {
		t.Run(test.name, func(t *testing.T) {
			oracle := regexp.MustCompile(test.pattern)
			compare := func(s string) {
				t.Helper()
				if got, want := test.check(s), oracle.MatchString(s); got != want {
					t.Fatalf("%q: got %v, want %v", s, got, want)
				}
			}
			for _, seed := range seeds {
				compare(seed)
				for _, pos := range []int{0, len(seed) / 2, len(seed)} {
					for b := 0; b < 256; b++ {
						compare(seed[:pos] + string([]byte{byte(b)}) + seed[pos:])
					}
				}
			}
			rng := rand.New(rand.NewSource(564))
			alphabet := []byte("aZ0-._~/\n\x00")
			for n := 0; n < 20000; n++ {
				s := make([]byte, rng.Intn(80))
				for i := range s {
					s[i] = alphabet[rng.Intn(len(alphabet))]
				}
				compare(string(s))
			}
		})
	}
}

func TestNameChecksKnownCases(t *testing.T) {
	for _, s := range []string{"a.b/c", "example.com/a-b/c.d_e~f", "A-B.c-D/E-F"} {
		if !CanonicalPath(s) {
			t.Fatalf("rejected %q", s)
		}
	}
	for _, s := range []string{"", "a/b", "a..b/c", "a.b//c", "a.b/c/", "a.b/_c", "a.b/c_", "a.b/c\n", "a.b/é"} {
		if CanonicalPath(s) {
			t.Fatalf("accepted %q", s)
		}
	}
	if !Slug("a._-") || Slug("A") || !Platform("linux/arm64") || Platform("linux/arm64/x") || !GitHubUser(strings.Repeat("a", 39)) || GitHubUser(strings.Repeat("a", 40)) {
		t.Fatal("name boundary mismatch")
	}
}

func TestNameChecksAllocateNothing(t *testing.T) {
	if n := testing.AllocsPerRun(100, func() {
		if !CanonicalPath("example.com/a-b/c.d_e~f") || !Slug("a._-") || !Platform("linux/arm64") || !GitHubUser("A-b0") {
			panic("valid name rejected")
		}
	}); n != 0 {
		t.Fatalf("name check allocations = %g", n)
	}
}

var benchmarkNameAccepted bool

func BenchmarkCanonicalPath(b *testing.B) {
	for _, s := range []string{"example.com/a-b/c.d_e~f", "example.com/a-b/c.d_e~f/"} {
		b.Run(s, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchmarkNameAccepted = CanonicalPath(s)
			}
		})
	}
}
