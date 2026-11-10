package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientBoundsHeaderAndBodyStalls(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "headers",
			handler: func(_ http.ResponseWriter, request *http.Request) {
				<-request.Context().Done()
			},
		},
		{
			name: "body",
			handler: func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte("x"))
				writer.(http.Flusher).Flush()
				<-request.Context().Done()
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()
			client := New(Config{Timeout: 75 * time.Millisecond})
			request, err := http.NewRequest(http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			_, err = client.Bytes(context.Background(), request, 16)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error = %v, want context deadline", err)
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("stalled request took %v", elapsed)
			}
		})
	}
}

func TestClientParentCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	client := New(Config{Timeout: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	done := make(chan error, 1)
	go func() {
		_, err := client.Bytes(ctx, request, 16)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled request did not return")
	}
}

func TestAPIClientDoesNotFollowRedirects(t *testing.T) {
	var sinkRequests atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		sinkRequests.Add(1)
	}))
	defer sink.Close()

	for _, status := range []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			redirector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Location", sink.URL)
				writer.WriteHeader(status)
			}))
			defer redirector.Close()

			request, err := http.NewRequest(http.MethodPost, redirector.URL, strings.NewReader("credential=secret"))
			if err != nil {
				t.Fatal(err)
			}
			response, err := NewAPI().Bytes(context.Background(), request, 1024)
			if err != nil {
				t.Fatalf("redirect response: %v", err)
			}
			if response.StatusCode != status {
				t.Fatalf("status = %d, want %d", response.StatusCode, status)
			}
		})
	}
	if got := sinkRequests.Load(); got != 0 {
		t.Fatalf("redirect sink received %d credential-bearing requests", got)
	}
}

func TestReadBoundedLimits(t *testing.T) {
	const limit = int64(8)
	if got, err := ReadBounded(strings.NewReader("12345678"), -1, limit, "test"); err != nil || string(got) != "12345678" {
		t.Fatalf("exact limit = %q, %v", got, err)
	}
	for name, length := range map[string]int64{"declared": 9, "unknown": -1} {
		t.Run(name, func(t *testing.T) {
			_, err := ReadBounded(strings.NewReader("123456789"), length, limit, "test")
			if !errors.Is(err, ErrBodyTooLarge) {
				t.Fatalf("error = %v, want ErrBodyTooLarge", err)
			}
		})
	}
}

func TestClientUsesIndependentErrorBodyLimit(t *testing.T) {
	client := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusBadGateway,
			Status:        "502 Bad Gateway",
			Header:        make(http.Header),
			Body:          io.NopCloser(strings.NewReader(strings.Repeat("x", 33))),
			ContentLength: -1,
			Request:       request,
		}, nil
	})}, Timeout: time.Second, ErrorBodyLimit: 32})
	request, _ := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
	response, err := client.Bytes(context.Background(), request, 1<<20)
	if response.StatusCode != http.StatusBadGateway || !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestClientRejectsDeclaredOversizeWithoutReadingAndCloses(t *testing.T) {
	body := &trackedBody{Reader: strings.NewReader("ignored")}
	client := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: body, ContentLength: 65, Request: request,
		}, nil
	})}, Timeout: time.Second})
	request, _ := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
	_, err := client.Bytes(context.Background(), request, 64)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("error = %v", err)
	}
	if body.reads.Load() != 0 || !body.closed.Load() {
		t.Fatalf("body reads=%d closed=%v", body.reads.Load(), body.closed.Load())
	}
}

func BenchmarkClientBytes1KiB(b *testing.B) {
	payload := strings.Repeat("x", 1024)
	client := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(payload)), ContentLength: int64(len(payload)), Request: request,
		}, nil
	})}, Timeout: time.Second})
	request, _ := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for range b.N {
		if _, err := client.Bytes(context.Background(), request, 2<<10); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzReadBounded(f *testing.F) {
	f.Add([]byte("small"), uint8(8))
	f.Add([]byte("too large"), uint8(3))
	f.Fuzz(func(t *testing.T, data []byte, rawLimit uint8) {
		limit := int64(rawLimit)
		got, err := ReadBounded(strings.NewReader(string(data)), -1, limit, "fuzz")
		if int64(len(data)) <= limit {
			if err != nil || string(got) != string(data) {
				t.Fatalf("bounded read = %q, %v", got, err)
			}
		} else if !errors.Is(err, ErrBodyTooLarge) {
			t.Fatalf("oversized error = %v", err)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

type trackedBody struct {
	io.Reader
	reads  atomic.Int32
	closed atomic.Bool
}

func (body *trackedBody) Read(buffer []byte) (int, error) {
	body.reads.Add(1)
	return body.Reader.Read(buffer)
}
func (body *trackedBody) Close() error {
	body.closed.Store(true)
	return nil
}
