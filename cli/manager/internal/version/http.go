package version

import (
	"context"
	"net/http"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/internal/httpclient"
)

const (
	releaseMetadataLimit int64 = 4 << 20
	checksumBodyLimit    int64 = 4 << 10
)

var (
	releaseMetadataHTTP    = httpclient.NewMetadata()
	releaseStreamHTTP      = httpclient.NewStreaming()
	releaseMetadataMaximum = releaseMetadataLimit
	checksumBodyMaximum    = checksumBodyLimit
)

func getReleaseBytes(ctx context.Context, operation, address string, limit int64) (httpclient.Response, error) {
	if err := automation.RequireOnline(operation); err != nil {
		return httpclient.Response{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return httpclient.Response{}, err
	}
	return releaseMetadataHTTP.Bytes(ctx, request, limit)
}

func openReleaseMetadata(ctx context.Context, operation, address string) (*http.Response, error) {
	if err := automation.RequireOnline(operation); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	return releaseMetadataHTTP.Open(ctx, request)
}

func openReleaseStream(ctx context.Context, operation, address string) (*http.Response, error) {
	if err := automation.RequireOnline(operation); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	return releaseStreamHTTP.Open(ctx, request)
}
