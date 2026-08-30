package manager_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiebiao/xiebiao.github.io/tools/media-manager/internal/manager"
)

type memoryObjects struct {
	objects map[string][]byte
}

type failingObjects struct {
	memoryObjects
	putCount int
	failAt   int
}

func (f *failingObjects) Put(ctx context.Context, object manager.Object) error {
	f.putCount++
	if f.putCount == f.failAt {
		return errors.New("simulated R2 outage")
	}
	return f.memoryObjects.Put(ctx, object)
}

func TestUserCanUpdateMetadataAndHugoManifest(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	repo, err := manager.OpenRepository(filepath.Join(tmp, "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	asset := manager.Asset{ID: "asset-1", Filename: "field.jpg", MIMEType: "image/jpeg", Checksum: "checksum-1",
		Width: 1200, Height: 800, Alt: "旧说明", Tags: []string{}, ArticleRefs: []string{},
		CreatedAt: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		UpdatedAt: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Variants:  []manager.Variant{{Width: 480, Height: 320, Format: "jpeg", Key: "posts/asset-1/480.jpeg", URL: "https://media.example/posts/asset-1/480.jpeg", Size: 8}}}
	if err := repo.Create(context.Background(), asset); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(tmp, "assets.json")
	handler, err := manager.NewServer(manager.ServerOptions{Repository: repo, Objects: &memoryObjects{}, Manifest: manifest})
	if err != nil {
		t.Fatal(err)
	}

	updateBody := bytes.NewBufferString(`{"alt":"新的替代文本","caption":"文章题图","takenAt":"2026-08-01","location":"青海","copyright":"Bill Xie","tags":["摄影","旅行"],"articleRefs":["posts/lenghu"]}`)
	request := httptest.NewRequest(http.MethodPatch, "/api/assets/asset-1", updateBody)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", response.Code, response.Body.String())
	}

	manifestBytes, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var exported map[string]manager.Asset
	if err := json.Unmarshal(manifestBytes, &exported); err != nil {
		t.Fatal(err)
	}
	if exported["asset-1"].Alt != "新的替代文本" || len(exported["asset-1"].ArticleRefs) != 1 {
		t.Fatalf("manifest was not updated: %+v", exported["asset-1"])
	}
}

func TestLinkedAssetMustBeUnlinkedBeforeDeletion(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	repo, err := manager.OpenRepository(filepath.Join(tmp, "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	asset := manager.Asset{ID: "linked", Filename: "field.jpg", MIMEType: "image/jpeg", Checksum: "checksum-linked",
		Width: 1200, Height: 800, Alt: "田野", Tags: []string{}, ArticleRefs: []string{"posts/field"},
		CreatedAt: "2026-08-29T12:00:00Z", UpdatedAt: "2026-08-29T12:00:00Z",
		Variants: []manager.Variant{{Width: 480, Height: 320, Format: "jpeg", Key: "posts/linked/480.jpeg", URL: "https://media.example/posts/linked/480.jpeg", Size: 8}}}
	if err := repo.Create(context.Background(), asset); err != nil {
		t.Fatal(err)
	}
	handler, err := manager.NewServer(manager.ServerOptions{Repository: repo, Objects: &memoryObjects{}, Manifest: filepath.Join(tmp, "assets.json")})
	if err != nil {
		t.Fatal(err)
	}

	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, httptest.NewRequest(http.MethodDelete, "/api/assets/linked", nil))
	if blocked.Code != http.StatusConflict {
		t.Fatalf("linked delete status = %d, want 409", blocked.Code)
	}

	update := bytes.NewBufferString(`{"alt":"田野","caption":"","takenAt":"","location":"","copyright":"","tags":[],"articleRefs":[]}`)
	updateRequest := httptest.NewRequest(http.MethodPatch, "/api/assets/linked", update)
	updateRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), updateRequest)

	deleted := httptest.NewRecorder()
	handler.ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, "/api/assets/linked", nil))
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("unlinked delete status = %d, body = %s", deleted.Code, deleted.Body.String())
	}
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/assets/linked", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("deleted asset GET status = %d, want 404", missing.Code)
	}
}

func (m *memoryObjects) Put(_ context.Context, object manager.Object) error {
	if m.objects == nil {
		m.objects = make(map[string][]byte)
	}
	if _, exists := m.objects[object.Key]; exists {
		return manager.ErrObjectExists
	}
	m.objects[object.Key] = append([]byte(nil), object.Body...)
	return nil
}

func (m *memoryObjects) Delete(_ context.Context, keys []string) error {
	for _, key := range keys {
		delete(m.objects, key)
	}
	return nil
}

func (m *memoryObjects) URL(key string) string { return "https://media.example/" + key }

func TestUserCanUploadAndRetrieveAnAsset(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	repo, err := manager.OpenRepository(filepath.Join(tmp, "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })

	objects := &memoryObjects{}
	handler, err := manager.NewServer(manager.ServerOptions{
		Repository: repo,
		Objects:    objects,
		Manifest:   filepath.Join(tmp, "assets.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("image", "field.jpg")
	imageBytes, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	_, _ = part.Write(imageBytes)
	_ = writer.WriteField("alt", "青海的田野")
	_ = writer.WriteField("caption", "夏末，冷湖")
	_ = writer.WriteField("tags", "摄影,青海")
	_ = writer.Close()

	upload := httptest.NewRequest(http.MethodPost, "/api/assets", &body)
	upload.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, upload)
	if response.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", response.Code, response.Body.String())
	}

	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/assets?q=%E9%9D%92%E6%B5%B7", nil))
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d", listResponse.Code)
	}
	var assets []manager.Asset
	if err := json.Unmarshal(listResponse.Body.Bytes(), &assets); err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 {
		t.Fatalf("got %d assets, want 1", len(assets))
	}
	if assets[0].Alt != "青海的田野" || !strings.Contains(assets[0].Variants[0].URL, "/cdn-cgi/image/width=1,quality=82,format=auto,fit=scale-down,onerror=redirect/") {
		t.Fatalf("unexpected asset: %+v", assets[0])
	}
	if len(objects.objects) != 1 {
		t.Fatalf("R2 objects = %d, want one original", len(objects.objects))
	}
}

func TestUploadRequiresAltText(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	repo, err := manager.OpenRepository(filepath.Join(tmp, "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := manager.NewServer(manager.ServerOptions{Repository: repo, Objects: &memoryObjects{}, Manifest: filepath.Join(tmp, "assets.json")})
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("image", "field.png")
	imageBytes, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	_, _ = part.Write(imageBytes)
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/assets", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing alt status = %d, want 400", response.Code)
	}
}

func TestPartialR2UploadIsRolledBack(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	repo, err := manager.OpenRepository(filepath.Join(tmp, "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	objects := &failingObjects{failAt: 1}
	handler, err := manager.NewServer(manager.ServerOptions{Repository: repo, Objects: objects, Manifest: filepath.Join(tmp, "assets.json")})
	if err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("image", "field.png")
	imageBytes, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	_, _ = part.Write(imageBytes)
	_ = writer.WriteField("alt", "田野")
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/assets", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	assets, err := repo.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 0 || len(objects.objects) != 0 {
		t.Fatalf("rollback left assets=%d objects=%d", len(assets), len(objects.objects))
	}
}

func TestDuplicateImageIsRejectedWithoutLeavingNewObjects(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	repo, err := manager.OpenRepository(filepath.Join(tmp, "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	objects := &memoryObjects{}
	handler, err := manager.NewServer(manager.ServerOptions{Repository: repo, Objects: objects, Manifest: filepath.Join(tmp, "assets.json")})
	if err != nil {
		t.Fatal(err)
	}
	imageBytes, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	uploadOnce := func() *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("image", "same.png")
		_, _ = part.Write(imageBytes)
		_ = writer.WriteField("alt", "田野")
		_ = writer.Close()
		request := httptest.NewRequest(http.MethodPost, "/api/assets", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if first := uploadOnce(); first.Code != http.StatusCreated {
		t.Fatalf("first upload = %d", first.Code)
	}
	objectCount := len(objects.objects)
	if duplicate := uploadOnce(); duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate upload = %d, body = %s", duplicate.Code, duplicate.Body.String())
	}
	if len(objects.objects) != objectCount {
		t.Fatalf("duplicate changed object count from %d to %d", objectCount, len(objects.objects))
	}
}

func TestUserCanRestoreCatalogFromExportedManifest(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	repo, err := manager.OpenRepository(filepath.Join(tmp, "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := manager.NewServer(manager.ServerOptions{Repository: repo, Objects: &memoryObjects{}, Manifest: filepath.Join(tmp, "assets.json")})
	if err != nil {
		t.Fatal(err)
	}
	manifest := `{"restored":{"id":"restored","filename":"field.jpg","mimeType":"image/jpeg","checksum":"restore-checksum","width":960,"height":640,"alt":"恢复的图片","caption":"","tags":["备份"],"articleRefs":[],"variants":[{"width":960,"height":640,"format":"auto","key":"posts/restored/original.jpeg#width=960","url":"https://media.example/cdn-cgi/image/width=960,quality=82,format=auto,fit=scale-down,onerror=redirect/posts/restored/original.jpeg","size":0}],"createdAt":"2026-08-29T12:00:00Z","updatedAt":"2026-08-29T12:00:00Z"}}`
	request := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(manifest))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("import status = %d, body = %s", response.Code, response.Body.String())
	}
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/assets/restored", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "恢复的图片") {
		t.Fatalf("restored GET status = %d, body = %s", get.Code, get.Body.String())
	}
}
