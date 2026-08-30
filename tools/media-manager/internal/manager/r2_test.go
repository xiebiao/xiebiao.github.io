package manager_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/xiebiao/xiebiao.github.io/tools/media-manager/internal/manager"
)

func TestR2StoreProtectsImmutableObjectsAndDeletesByKey(t *testing.T) {
	t.Parallel()
	var methods []string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Errorf("missing SigV4 authorization")
		}
		if r.Method == http.MethodPut {
			if r.Header.Get("If-None-Match") != "*" {
				t.Errorf("If-None-Match = %q", r.Header.Get("If-None-Match"))
			}
			if r.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
				t.Errorf("Cache-Control = %q", r.Header.Get("Cache-Control"))
			}
			_, _ = io.Copy(io.Discard, r.Body)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Status: "204 No Content", Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})

	store, err := manager.NewR2Store(manager.R2Config{
		Endpoint: "https://r2.example", Bucket: "media", AccessKeyID: "access", SecretAccessKey: "secret",
		PublicBaseURL: "https://media.example",
		Client:        &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatal(err)
	}
	object := manager.Object{Key: "posts/a/480.jpeg", Body: []byte("image"), ContentType: "image/jpeg", Metadata: map[string]string{"asset-id": "a"}}
	if err := store.Put(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), []string{object.Key}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(methods, ","); got != "PUT /media/posts/a/480.jpeg,DELETE /media/posts/a/480.jpeg" {
		t.Fatalf("requests = %s", got)
	}
	if got := store.URL(object.Key); got != "https://media.example/posts/a/480.jpeg" {
		t.Fatalf("public URL = %s", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
