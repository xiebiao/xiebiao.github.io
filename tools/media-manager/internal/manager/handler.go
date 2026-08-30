package manager

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed web/index.html
var managerUI []byte

const maxUploadBytes = 50 << 20

type ServerOptions struct {
	Repository *Repository
	Objects    ObjectStore
	Manifest   string
}

type server struct{ options ServerOptions }

func NewServer(options ServerOptions) (http.Handler, error) {
	if options.Repository == nil || options.Objects == nil || options.Manifest == "" {
		return nil, errors.New("repository, object store, and manifest are required")
	}
	return securityHeaders(&server{options: options}), nil
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/" && r.Method == http.MethodGet:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(managerUI)
	case r.URL.Path == "/healthz" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case r.URL.Path == "/api/assets" && r.Method == http.MethodGet:
		s.list(w, r)
	case r.URL.Path == "/api/assets" && r.Method == http.MethodPost:
		s.upload(w, r)
	case r.URL.Path == "/api/export" && r.Method == http.MethodPost:
		if err := exportManifest(r.Context(), s.options.Repository, s.options.Manifest); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"manifest": s.options.Manifest})
	case r.URL.Path == "/api/import" && r.Method == http.MethodPost:
		s.importManifest(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/assets/"):
		s.asset(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *server) importManifest(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 20<<20))
	decoder.DisallowUnknownFields()
	var manifest map[string]Asset
	if err := decoder.Decode(&manifest); err != nil {
		writeError(w, http.StatusBadRequest, "invalid manifest: "+err.Error())
		return
	}
	if len(manifest) == 0 {
		writeError(w, http.StatusBadRequest, "manifest must contain at least one asset")
		return
	}
	ids := make([]string, 0, len(manifest))
	for id := range manifest {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	assets := make([]Asset, 0, len(ids))
	for _, id := range ids {
		asset := manifest[id]
		if asset.ID != id || asset.Filename == "" || asset.Checksum == "" || asset.Width < 1 || asset.Height < 1 || len(asset.Variants) == 0 {
			writeError(w, http.StatusBadRequest, "manifest contains an invalid asset: "+id)
			return
		}
		for _, variant := range asset.Variants {
			if variant.Width < 1 || variant.Height < 1 || variant.Key == "" || variant.URL == "" || variant.Format != "auto" {
				writeError(w, http.StatusBadRequest, "manifest contains an invalid variant: "+id)
				return
			}
		}
		assets = append(assets, asset)
	}
	if err := s.options.Repository.ReplaceAll(r.Context(), assets); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := exportManifest(r.Context(), s.options.Repository, s.options.Manifest); err != nil {
		writeError(w, http.StatusInternalServerError, "catalog restored but manifest rewrite failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"assets": len(assets)})
}

func (s *server) asset(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/assets/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		asset, err := s.options.Repository.Get(r.Context(), id)
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "asset not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, asset)
	case http.MethodPatch:
		var update MetadataUpdate
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&update); err != nil {
			writeError(w, http.StatusBadRequest, "invalid metadata: "+err.Error())
			return
		}
		if strings.TrimSpace(update.Alt) == "" {
			writeError(w, http.StatusBadRequest, "alt text is required")
			return
		}
		asset, err := s.options.Repository.Update(r.Context(), id, update, time.Now().UTC().Format(time.RFC3339))
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "asset not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := exportManifest(r.Context(), s.options.Repository, s.options.Manifest); err != nil {
			writeError(w, http.StatusInternalServerError, "metadata saved but manifest export failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, asset)
	case http.MethodDelete:
		asset, err := s.options.Repository.Get(r.Context(), id)
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "asset not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(asset.ArticleRefs) > 0 {
			writeError(w, http.StatusConflict, "remove article references before deleting this asset")
			return
		}
		keys := []string{fmt.Sprintf("posts/%s/original.%s", asset.ID, extensionForMIME(asset.MIMEType))}
		if err := s.options.Objects.Delete(r.Context(), keys); err != nil {
			writeError(w, http.StatusBadGateway, "R2 delete failed; the catalog was left unchanged")
			return
		}
		if err := s.options.Repository.Delete(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := exportManifest(r.Context(), s.options.Repository, s.options.Manifest); err != nil {
			writeError(w, http.StatusInternalServerError, "asset deleted but manifest export failed: "+err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PATCH, DELETE")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) list(w http.ResponseWriter, r *http.Request) {
	assets, err := s.options.Repository.List(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, assets)
}

func (s *server) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid upload or file exceeds 50 MiB")
		return
	}
	if strings.TrimSpace(r.FormValue("alt")) == "" {
		writeError(w, http.StatusBadRequest, "alt text is required")
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "image is required")
		return
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read image")
		return
	}
	mimeType := http.DetectContentType(body)
	if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/gif" {
		writeError(w, http.StatusUnsupportedMediaType, "supported source formats are JPEG, PNG, and GIF")
		return
	}
	dimensions, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "cannot read image dimensions: "+err.Error())
		return
	}
	width, height := displayDimensions(r, dimensions.Width, dimensions.Height)
	id, err := newID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hash := sha256.Sum256(body)
	now := time.Now().UTC().Format(time.RFC3339)
	asset := Asset{
		ID: id, Filename: filepath.Base(header.Filename), MIMEType: mimeType, Checksum: hex.EncodeToString(hash[:]),
		Width: width, Height: height, Alt: strings.TrimSpace(r.FormValue("alt")),
		Caption: strings.TrimSpace(r.FormValue("caption")), TakenAt: strings.TrimSpace(r.FormValue("takenAt")),
		Location: strings.TrimSpace(r.FormValue("location")), Copyright: strings.TrimSpace(r.FormValue("copyright")),
		Tags: splitList(r.FormValue("tags")), ArticleRefs: splitList(r.FormValue("articleRefs")), CreatedAt: now, UpdatedAt: now,
	}
	metadata := objectMetadata(asset)
	originalExt := extensionForMIME(mimeType)
	originalKey := fmt.Sprintf("posts/%s/original.%s", id, originalExt)
	if err := s.options.Objects.Put(r.Context(), Object{Key: originalKey, Body: body, ContentType: mimeType, Metadata: metadata}); err != nil {
		writeError(w, http.StatusBadGateway, "R2 upload failed: "+err.Error())
		return
	}
	asset.Variants = cloudflareVariants(s.options.Objects.URL(originalKey), originalKey, width, height)
	if err := s.options.Repository.Create(r.Context(), asset); err != nil {
		_ = s.options.Objects.Delete(context.WithoutCancel(r.Context()), []string{originalKey})
		if errors.Is(err, ErrDuplicate) {
			writeError(w, http.StatusConflict, "this image has already been uploaded")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := exportManifest(r.Context(), s.options.Repository, s.options.Manifest); err != nil {
		writeError(w, http.StatusInternalServerError, "asset saved but manifest export failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, asset)
}

func exportManifest(ctx context.Context, repository *Repository, destination string) error {
	assets, err := repository.List(ctx, "")
	if err != nil {
		return err
	}
	manifest := make(map[string]Asset, len(assets))
	for _, asset := range assets {
		sort.Slice(asset.Variants, func(i, j int) bool {
			if asset.Variants[i].Width == asset.Variants[j].Width {
				return asset.Variants[i].Format < asset.Variants[j].Format
			}
			return asset.Variants[i].Width < asset.Variants[j].Width
		})
		manifest[asset.ID] = asset
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".assets-*.json")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err = temp.Write(append(encoded, '\n')); err == nil {
		err = temp.Close()
	} else {
		_ = temp.Close()
	}
	if err != nil {
		return err
	}
	return os.Rename(tempName, destination)
}

func objectMetadata(asset Asset) map[string]string {
	return map[string]string{
		"asset-id": asset.ID, "checksum": asset.Checksum,
		"filename": url.QueryEscape(asset.Filename), "alt": url.QueryEscape(asset.Alt),
		"caption": url.QueryEscape(asset.Caption),
	}
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '，' || r == '\n' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func extensionForMIME(value string) string {
	switch value {
	case "image/jpeg":
		return "jpeg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	}
	return "bin"
}

func displayDimensions(r *http.Request, fallbackWidth, fallbackHeight int) (int, int) {
	width, widthErr := strconv.Atoi(r.FormValue("displayWidth"))
	height, heightErr := strconv.Atoi(r.FormValue("displayHeight"))
	if widthErr == nil && heightErr == nil && width > 0 && height > 0 && width <= 100000 && height <= 100000 {
		return width, height
	}
	return fallbackWidth, fallbackHeight
}

func cloudflareVariants(originalURL, originalKey string, sourceWidth, sourceHeight int) []Variant {
	base := strings.TrimSuffix(originalURL, originalKey)
	widths := []int{480, 960, 1440, 1920}
	variants := make([]Variant, 0, len(widths))
	for _, width := range widths {
		if width >= sourceWidth {
			width = sourceWidth
		}
		height := max(1, int(float64(sourceHeight)*float64(width)/float64(sourceWidth)+0.5))
		variants = append(variants, Variant{
			Width: width, Height: height, Format: "auto", Key: fmt.Sprintf("%s#width=%d", originalKey, width),
			URL: fmt.Sprintf("%scdn-cgi/image/width=%d,quality=82,format=auto,fit=scale-down,onerror=redirect/%s", base, width, originalKey),
		})
		if width == sourceWidth {
			break
		}
	}
	return variants
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
