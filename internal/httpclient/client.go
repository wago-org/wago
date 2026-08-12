// Package httpclient provides the bounded, context-aware HTTP policies used by
// Wago's command-line network operations.
package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const DefaultErrorBodyLimit int64 = 64 << 10

var ErrBodyTooLarge = errors.New("HTTP response body exceeds limit")

// BodyTooLargeError reports a response that exceeded its configured in-memory
// or streaming limit.
type BodyTooLargeError struct {
	URL           string
	Limit         int64
	ContentLength int64
}

func (e *BodyTooLargeError) Error() string {
	if e.ContentLength >= 0 {
		return fmt.Sprintf("HTTP response from %s is too large: %d bytes exceeds %d-byte limit", e.URL, e.ContentLength, e.Limit)
	}
	return fmt.Sprintf("HTTP response from %s exceeds %d-byte limit", e.URL, e.Limit)
}

func (e *BodyTooLargeError) Unwrap() error { return ErrBodyTooLarge }

// Config configures one operation class. Timeout is the overall operation
// deadline; transport fields independently bound connection and header stalls.
type Config struct {
	HTTPClient            *http.Client
	Timeout               time.Duration
	ErrorBodyLimit        int64
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	IdleConnTimeout       time.Duration
	ExpectContinueTimeout time.Duration
}

// Client applies one explicit timeout/transport policy. It is safe for
// concurrent use.
type Client struct {
	httpClient     *http.Client
	timeout        time.Duration
	errorBodyLimit int64
}

// Response is a fully read, bounded response.
type Response struct {
	StatusCode int
	Status     string
	Header     http.Header
	Body       []byte
}

func New(config Config) *Client {
	if config.ErrorBodyLimit <= 0 {
		config.ErrorBodyLimit = DefaultErrorBodyLimit
	}
	if config.DialTimeout <= 0 {
		config.DialTimeout = 10 * time.Second
	}
	if config.TLSHandshakeTimeout <= 0 {
		config.TLSHandshakeTimeout = 10 * time.Second
	}
	if config.ResponseHeaderTimeout <= 0 {
		config.ResponseHeaderTimeout = 15 * time.Second
	}
	if config.IdleConnTimeout <= 0 {
		config.IdleConnTimeout = 90 * time.Second
	}
	if config.ExpectContinueTimeout <= 0 {
		config.ExpectContinueTimeout = time.Second
	}
	client := config.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.DialContext = (&net.Dialer{
			Timeout:   config.DialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext
		transport.TLSHandshakeTimeout = config.TLSHandshakeTimeout
		transport.ResponseHeaderTimeout = config.ResponseHeaderTimeout
		transport.IdleConnTimeout = config.IdleConnTimeout
		transport.ExpectContinueTimeout = config.ExpectContinueTimeout
		client = &http.Client{Transport: transport}
	}
	return &Client{httpClient: client, timeout: config.Timeout, errorBodyLimit: config.ErrorBodyLimit}
}

// NewAPI returns the policy for small registry/API/OAuth JSON operations.
func NewAPI() *Client {
	return New(Config{Timeout: 30 * time.Second, ResponseHeaderTimeout: 15 * time.Second})
}

// NewMetadata returns the policy for release metadata and checksums.
func NewMetadata() *Client {
	return New(Config{Timeout: 45 * time.Second, ResponseHeaderTimeout: 20 * time.Second})
}

// NewStreaming returns the policy for bounded release/source-archive streams.
// The longer overall timeout accommodates slow links; connect, TLS, and header
// waits remain independently bounded.
func NewStreaming() *Client {
	return New(Config{Timeout: 30 * time.Minute, ResponseHeaderTimeout: 30 * time.Second})
}

// Open executes request with the supplied parent context and this client's
// overall deadline. Closing the returned body releases the deadline context.
func (c *Client) Open(ctx context.Context, request *http.Request) (*http.Response, error) {
	if ctx == nil {
		return nil, errors.New("nil HTTP request context")
	}
	operationContext := ctx
	cancel := func() {}
	if c.timeout > 0 {
		operationContext, cancel = context.WithTimeout(ctx, c.timeout)
	}
	request = request.Clone(operationContext)
	response, err := c.httpClient.Do(request)
	if err != nil {
		cancel()
		return nil, err
	}
	response.Body = &cancelReadCloser{ReadCloser: response.Body, cancel: cancel}
	return response, nil
}

// Bytes executes and completely reads a small response. Successful responses
// use limit; non-success responses use the independently bounded error-body
// limit. The response body is always closed.
func (c *Client) Bytes(ctx context.Context, request *http.Request, limit int64) (Response, error) {
	response, err := c.Open(ctx, request)
	if err != nil {
		return Response{}, err
	}
	defer response.Body.Close()
	result := Response{
		StatusCode: response.StatusCode,
		Status:     response.Status,
		Header:     response.Header.Clone(),
	}
	bodyLimit := limit
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		bodyLimit = c.errorBodyLimit
	}
	result.Body, err = ReadBounded(response.Body, response.ContentLength, bodyLimit, request.URL.String())
	if err != nil {
		return result, err
	}
	return result, nil
}

// ReadBounded reads at most limit bytes. It rejects an oversized declared
// Content-Length before reading and also enforces the limit for chunked or
// dishonest responses.
func ReadBounded(body io.Reader, contentLength, limit int64, source string) ([]byte, error) {
	if limit < 0 {
		return nil, fmt.Errorf("invalid HTTP body limit %d", limit)
	}
	if contentLength > limit {
		return nil, &BodyTooLargeError{URL: source, Limit: limit, ContentLength: contentLength}
	}
	limited := io.LimitReader(body, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, &BodyTooLargeError{URL: source, Limit: limit, ContentLength: -1}
	}
	return data, nil
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel func()
}

func (body *cancelReadCloser) Close() error {
	err := body.ReadCloser.Close()
	body.cancel()
	return err
}
