package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type releaseTransport func(*http.Request) (*http.Response, error)

func (f releaseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDownloadUsesLatestReleaseSnapshot(t *testing.T) {
	const asset = "sing-box-panel-linux-arm64.tar.gz"
	const data = "test installation archive"
	const assetURL = "https://github.com/ruiange/sing-box-panel/releases/download/v0.1.3/" + asset
	var requests []string
	client := &http.Client{Transport: releaseTransport(func(req *http.Request) (*http.Response, error) {
		address := req.URL.String()
		requests = append(requests, address)
		body := data
		switch address {
		case latestURL:
			body = fmt.Sprintf(`{"tag_name":"v0.1.3","assets":[{"name":%q,"state":"uploaded","size":%d,"digest":"sha256:%x"}]}`,
				asset, len(data), sha256.Sum256([]byte(data)))
		case assetURL:
		default:
			t.Fatalf("unexpected request: %s", address)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}

	ctx := context.Background()
	release, err := getLatestRelease(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path, err := downloadRelease(ctx, client, release, "arm64", dir)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != data || path != filepath.Join(dir, asset) {
		t.Fatalf("unexpected downloaded file: path=%q data=%q err=%v", path, content, err)
	}
	if _, err := downloadRelease(ctx, client, release, "arm64", dir); err == nil {
		t.Fatal("download accepted an existing file")
	}
	if len(requests) != 2 || requests[0] != latestURL || requests[1] != assetURL {
		t.Fatalf("unexpected requests: %v", requests)
	}
}

func TestDownloadRemovesTemporaryFileOnChecksumMismatch(t *testing.T) {
	release := releaseInfo{Tag: "v0.1.3", Assets: []releaseAsset{{
		Name: "sing-box-panel-linux-amd64.tar.gz", State: "uploaded", Size: 4,
		Digest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("good"))),
	}}}
	client := &http.Client{Transport: releaseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("evil"))}, nil
	})}

	dir := t.TempDir()
	_, err := downloadRelease(context.Background(), client, release, "amd64", dir)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("expected checksum failure, got %v", err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("download failure left files behind: %v, %v", files, err)
	}
}
