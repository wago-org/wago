package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEditDistance(t *testing.T) {
	var previous, current [maximumSuggestionIDLength + 1]int
	if got, ok := editDistanceWithin("wago-org/asi", "wago-org/wasi", 3, previous[:], current[:]); !ok || got != 1 {
		t.Fatalf("edit distance = %d, want 1", got)
	}
	if got, ok := editDistanceWithin("wasi", "wasi", 3, previous[:], current[:]); !ok || got != 0 {
		t.Fatalf("identical edit distance = %d, want 0", got)
	}
	if _, ok := editDistanceWithin("short", strings.Repeat("x", 20), 3, previous[:], current[:]); ok {
		t.Fatal("out-of-band edit distance succeeded")
	}
}

func TestEditDistanceWithinMatchesExactDistance(t *testing.T) {
	values := []string{""}
	for length := 1; length <= 4; length++ {
		for bits := 0; bits < 1<<length; bits++ {
			var value strings.Builder
			for index := range length {
				if bits&(1<<index) == 0 {
					value.WriteByte('a')
				} else {
					value.WriteByte('b')
				}
			}
			values = append(values, value.String())
		}
	}
	var previous, current [maximumSuggestionIDLength + 1]int
	for _, left := range values {
		for _, right := range values {
			want := exactEditDistance(left, right)
			for limit := 0; limit <= 3; limit++ {
				got, ok := editDistanceWithin(left, right, limit, previous[:], current[:])
				if ok != (want <= limit) || ok && got != want {
					t.Fatalf("distance(%q, %q, %d) = %d, %t; want %d", left, right, limit, got, ok, want)
				}
			}
		}
	}
}

func exactEditDistance(left, right string) int {
	previous := make([]int, len(right)+1)
	for index := range previous {
		previous[index] = index
	}
	for i := 1; i <= len(left); i++ {
		current := make([]int, len(right)+1)
		current[0] = i
		for j := 1; j <= len(right); j++ {
			cost := 1
			if left[i-1] == right[j-1] {
				cost = 0
			}
			current[j] = min(current[j-1]+1, previous[j]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(right)]
}

func BenchmarkClosestModuleAdversarialID(b *testing.B) {
	target := strings.Repeat("a", 300)
	candidates := []packageSuggestion{{ID: strings.Repeat("b", 10_000)}}
	b.ReportAllocs()
	for range b.N {
		_ = closestModuleID(target, candidates)
	}
}

func TestClosestModuleRejectsOversizedMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(output http.ResponseWriter, _ *http.Request) {
		_, _ = output.Write([]byte(`{"packages":[` + strings.Repeat(`{"id":"x"},`, maximumRegistrySuggestions) + `{"id":"x"}]}`))
	}))
	defer server.Close()
	t.Setenv("WAGO_REGISTRY", server.URL)

	if got := closestModule("github.com/wago-org/asi"); got != "" {
		t.Fatalf("oversized package suggestions = %q", got)
	}
}

func TestClosestModuleIgnoresInvalidCandidateID(t *testing.T) {
	candidates := []packageSuggestion{{ID: "wago-org/asi\x1b[2J"}, {ID: strings.Repeat("x", maximumSuggestionIDLength+1)}}
	if got := closestModuleID("wago-org/asj", candidates); got != "" {
		t.Fatalf("invalid package suggestion = %q", got)
	}
}

func TestClosestModule(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(output http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/packages" {
			t.Errorf("request path = %q", request.URL.Path)
		}
		_, _ = output.Write([]byte(`{"packages":[{"id":"wago-org/wasi"},{"id":"acme/logger"}]}`))
	}))
	defer server.Close()
	t.Setenv("WAGO_REGISTRY", server.URL)

	if got := closestModule("github.com/wago-org/asi"); got != "wago-org/wasi" {
		t.Fatalf("closest module = %q, want wago-org/wasi", got)
	}
	if got := closestModule("github.com/unrelated/package"); got != "" {
		t.Fatalf("unrelated suggestion = %q, want none", got)
	}
}

func TestResolveInstallPackageReturnsPublishedSubpackages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(output http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/packages/github.com/acme/tools" {
			t.Errorf("request path = %q", request.URL.Path)
		}
		_, _ = output.Write([]byte(`{
			"module":"github.com/acme/tools",
			"displayName":"Acme Tools",
			"subpackages":[
				{"module":"github.com/acme/tools/log","name":"Logging","description":"Write logs.","stability":"stable"},
				{"module":"github.com/acme/tools/metrics","name":"Metrics","description":"Record metrics.","stability":"experimental"}
			]
		}`))
	}))
	defer server.Close()
	t.Setenv("WAGO_REGISTRY", server.URL)

	pkg, found, err := ResolveInstallPackage(context.Background(), "github.com/acme/tools")
	if err != nil || !found {
		t.Fatalf("resolve = %#v, %t, %v", pkg, found, err)
	}
	if pkg.Name != "Acme Tools" || len(pkg.Subpackages) != 2 || pkg.Subpackages[1].Module != "github.com/acme/tools/metrics" {
		t.Fatalf("package = %#v", pkg)
	}
}

func TestResolveInstallPackageTreatsProviderIDAsNonPackage(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	t.Setenv("WAGO_REGISTRY", server.URL)

	if pkg, found, err := ResolveInstallPackage(context.Background(), "github.com/acme/tools/log"); err != nil || found || pkg.Module != "" {
		t.Fatalf("resolve = %#v, %t, %v", pkg, found, err)
	}
}

func TestResolveInstallPackageRejectsForeignSubpackage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(output http.ResponseWriter, _ *http.Request) {
		_, _ = output.Write([]byte(`{"module":"github.com/acme/tools","displayName":"Tools","subpackages":[{"module":"github.com/other/package","name":"Other","description":"Other.","stability":"stable"}]}`))
	}))
	defer server.Close()
	t.Setenv("WAGO_REGISTRY", server.URL)

	if _, _, err := ResolveInstallPackage(context.Background(), "github.com/acme/tools"); err == nil {
		t.Fatal("accepted foreign subpackage")
	}
}
