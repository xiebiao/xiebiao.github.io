package manager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxTransformedBytes = 50 << 20

type CloudflareTransformer struct {
	client *http.Client
}

func NewCloudflareTransformer(client *http.Client) *CloudflareTransformer {
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	return &CloudflareTransformer{client: client}
}

func (t *CloudflareTransformer) TransformWebP(ctx context.Context, sourceURL string, width int) ([]byte, error) {
	transformURL, err := cloudflareTransformURL(sourceURL, width)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, transformURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "image/webp")
	response, err := t.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return nil, fmt.Errorf("Cloudflare returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "image/webp" {
		return nil, fmt.Errorf("Cloudflare returned %q instead of image/webp", response.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTransformedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, errors.New("Cloudflare returned an empty image")
	}
	if len(body) > maxTransformedBytes {
		return nil, errors.New("Cloudflare transformed image exceeds 50 MiB")
	}
	return body, nil
}

func cloudflareTransformURL(sourceURL string, width int) (string, error) {
	if width < 1 {
		return "", errors.New("transform width must be positive")
	}
	parsed, err := url.Parse(sourceURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return "", errors.New("invalid source image URL")
	}
	path := strings.TrimLeft(parsed.EscapedPath(), "/")
	if path == "" {
		return "", errors.New("source image URL must contain an object path")
	}
	options := "width=" + strconv.Itoa(width) + ",quality=82,format=webp,fit=scale-down"
	return parsed.Scheme + "://" + parsed.Host + "/cdn-cgi/image/" + options + "/" + path, nil
}
