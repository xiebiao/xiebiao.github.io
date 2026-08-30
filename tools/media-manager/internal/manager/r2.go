package manager

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type R2Config struct {
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	PublicBaseURL   string
	Client          *http.Client
}

type R2Store struct {
	config R2Config
	client *http.Client
	now    func() time.Time
}

func NewR2Store(config R2Config) (*R2Store, error) {
	if config.Endpoint == "" || config.Bucket == "" || config.AccessKeyID == "" || config.SecretAccessKey == "" || config.PublicBaseURL == "" {
		return nil, errors.New("R2 endpoint, bucket, credentials, and public base URL are required")
	}
	parsed, err := url.Parse(config.Endpoint)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return nil, errors.New("invalid R2 endpoint")
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	return &R2Store{config: config, client: client, now: time.Now}, nil
}

func (s *R2Store) Put(ctx context.Context, object Object) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, s.objectURL(object.Key), bytes.NewReader(object.Body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", object.ContentType)
	request.Header.Set("Cache-Control", "public, max-age=31536000, immutable")
	request.Header.Set("If-None-Match", "*")
	for key, value := range object.Metadata {
		request.Header.Set("X-Amz-Meta-"+key, value)
	}
	s.sign(request, object.Body)
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusPreconditionFailed {
		return ErrObjectExists
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response)
	}
	return nil
}

func (s *R2Store) Delete(ctx context.Context, keys []string) error {
	for _, key := range keys {
		request, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.objectURL(key), nil)
		if err != nil {
			return err
		}
		s.sign(request, nil)
		response, err := s.client.Do(request)
		if err != nil {
			return err
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			err = responseError(response)
			response.Body.Close()
			return err
		}
		response.Body.Close()
	}
	return nil
}

func (s *R2Store) URL(key string) string {
	return strings.TrimRight(s.config.PublicBaseURL, "/") + "/" + strings.TrimLeft(key, "/")
}

func (s *R2Store) objectURL(key string) string {
	segments := strings.Split(strings.TrimLeft(key, "/"), "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	return strings.TrimRight(s.config.Endpoint, "/") + "/" + url.PathEscape(s.config.Bucket) + "/" + strings.Join(segments, "/")
}

func (s *R2Store) sign(request *http.Request, payload []byte) {
	now := s.now().UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	payloadSum := sha256.Sum256(payload)
	payloadHash := hex.EncodeToString(payloadSum[:])
	request.Header.Set("X-Amz-Date", amzDate)
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)

	headers := map[string]string{"host": request.URL.Host}
	for key, values := range request.Header {
		lower := strings.ToLower(key)
		if lower == "authorization" {
			continue
		}
		headers[lower] = strings.Join(values, ",")
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var canonicalHeaders strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&canonicalHeaders, "%s:%s\n", key, strings.Join(strings.Fields(headers[key]), " "))
	}
	signedHeaders := strings.Join(keys, ";")
	canonicalRequest := strings.Join([]string{
		request.Method,
		request.URL.EscapedPath(),
		request.URL.Query().Encode(),
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")
	canonicalSum := sha256.Sum256([]byte(canonicalRequest))
	scope := date + "/auto/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(canonicalSum[:])
	dateKey := hmacSHA256([]byte("AWS4"+s.config.SecretAccessKey), date)
	regionKey := hmacSHA256(dateKey, "auto")
	serviceKey := hmacSHA256(regionKey, "s3")
	signingKey := hmacSHA256(serviceKey, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	request.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.config.AccessKeyID, scope, signedHeaders, signature))
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func responseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return fmt.Errorf("R2 returned %s: %s", response.Status, strings.TrimSpace(string(body)))
}
