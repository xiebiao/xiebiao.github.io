package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type Repository struct {
	path string
	mu   sync.Mutex
}

func OpenRepository(path string) (*Repository, error) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return nil, errors.New("sqlite3 command is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	repository := &Repository{path: path}
	_, err := repository.run(context.Background(), `PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;
CREATE TABLE IF NOT EXISTS assets (
  id TEXT PRIMARY KEY,
  filename TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  checksum TEXT NOT NULL UNIQUE,
  width INTEGER NOT NULL,
  height INTEGER NOT NULL,
  alt TEXT NOT NULL,
  caption TEXT NOT NULL,
  taken_at TEXT NOT NULL DEFAULT '',
  location TEXT NOT NULL DEFAULT '',
  copyright TEXT NOT NULL DEFAULT '',
  tags TEXT NOT NULL DEFAULT '[]',
  article_refs TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS variants (
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  width INTEGER NOT NULL,
  height INTEGER NOT NULL,
  format TEXT NOT NULL,
  object_key TEXT NOT NULL UNIQUE,
  url TEXT NOT NULL,
  size INTEGER NOT NULL,
  PRIMARY KEY (asset_id, width, format)
);`)
	if err != nil {
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return repository, nil
}

func (r *Repository) Close() error { return nil }

func (r *Repository) Create(ctx context.Context, asset Asset) error {
	var statement strings.Builder
	statement.WriteString("PRAGMA foreign_keys = ON; BEGIN IMMEDIATE;\n")
	appendAssetInsert(&statement, asset)
	statement.WriteString("COMMIT;")
	_, err := r.run(ctx, statement.String())
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: assets.checksum") {
		return ErrDuplicate
	}
	return err
}

func (r *Repository) ReplaceAll(ctx context.Context, assets []Asset) error {
	var statement strings.Builder
	statement.WriteString("PRAGMA foreign_keys = ON; BEGIN IMMEDIATE; DELETE FROM variants; DELETE FROM assets;\n")
	for _, asset := range assets {
		appendAssetInsert(&statement, asset)
	}
	statement.WriteString("COMMIT;")
	_, err := r.run(ctx, statement.String())
	return err
}

func appendAssetInsert(statement *strings.Builder, asset Asset) {
	tags, _ := json.Marshal(normalizeList(asset.Tags))
	refs, _ := json.Marshal(normalizeList(asset.ArticleRefs))
	fmt.Fprintf(statement, `INSERT INTO assets
(id, filename, mime_type, checksum, width, height, alt, caption, taken_at, location, copyright, tags, article_refs, created_at, updated_at)
VALUES (%s, %s, %s, %s, %d, %d, %s, %s, %s, %s, %s, %s, %s, %s, %s);`+"\n",
		quote(asset.ID), quote(asset.Filename), quote(asset.MIMEType), quote(asset.Checksum), asset.Width, asset.Height,
		quote(asset.Alt), quote(asset.Caption), quote(asset.TakenAt), quote(asset.Location), quote(asset.Copyright),
		quote(string(tags)), quote(string(refs)), quote(asset.CreatedAt), quote(asset.UpdatedAt))
	for _, variant := range asset.Variants {
		fmt.Fprintf(statement, `INSERT INTO variants (asset_id, width, height, format, object_key, url, size)
VALUES (%s, %d, %d, %s, %s, %s, %d);`+"\n", quote(asset.ID), variant.Width, variant.Height,
			quote(variant.Format), quote(variant.Key), quote(variant.URL), variant.Size)
	}
}

func (r *Repository) List(ctx context.Context, query string) ([]Asset, error) {
	pattern := "%" + query + "%"
	statement := fmt.Sprintf(`SELECT id, filename, mime_type AS mimeType, checksum, width, height, alt, caption,
taken_at AS takenAt, location, copyright, tags, article_refs AS articleRefs, created_at AS createdAt, updated_at AS updatedAt
FROM assets WHERE %s = '' OR filename LIKE %s OR alt LIKE %s OR caption LIKE %s OR location LIKE %s OR tags LIKE %s
ORDER BY created_at DESC;`, quote(query), quote(pattern), quote(pattern), quote(pattern), quote(pattern), quote(pattern))
	output, err := r.query(ctx, statement)
	if err != nil {
		return nil, err
	}
	var rows []assetRow
	if len(output) > 0 {
		if err := json.Unmarshal(output, &rows); err != nil {
			return nil, err
		}
	}
	assets := make([]Asset, 0, len(rows))
	for _, row := range rows {
		asset := row.asset()
		asset.Variants, err = r.variants(ctx, asset.ID)
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func (r *Repository) Get(ctx context.Context, id string) (Asset, error) {
	output, err := r.query(ctx, fmt.Sprintf(`SELECT id, filename, mime_type AS mimeType, checksum, width, height, alt, caption,
taken_at AS takenAt, location, copyright, tags, article_refs AS articleRefs, created_at AS createdAt, updated_at AS updatedAt
FROM assets WHERE id = %s;`, quote(id)))
	if err != nil {
		return Asset{}, err
	}
	var rows []assetRow
	if len(output) > 0 {
		if err := json.Unmarshal(output, &rows); err != nil {
			return Asset{}, err
		}
	}
	if len(rows) == 0 {
		return Asset{}, ErrNotFound
	}
	asset := rows[0].asset()
	asset.Variants, err = r.variants(ctx, id)
	return asset, err
}

func (r *Repository) Update(ctx context.Context, id string, update MetadataUpdate, updatedAt string) (Asset, error) {
	tags, _ := json.Marshal(normalizeList(update.Tags))
	refs, _ := json.Marshal(normalizeList(update.ArticleRefs))
	statement := fmt.Sprintf(`UPDATE assets SET alt = %s, caption = %s, taken_at = %s, location = %s,
copyright = %s, tags = %s, article_refs = %s, updated_at = %s WHERE id = %s;
SELECT changes();`, quote(strings.TrimSpace(update.Alt)), quote(strings.TrimSpace(update.Caption)), quote(strings.TrimSpace(update.TakenAt)),
		quote(strings.TrimSpace(update.Location)), quote(strings.TrimSpace(update.Copyright)), quote(string(tags)), quote(string(refs)),
		quote(updatedAt), quote(id))
	output, err := r.query(ctx, statement)
	if err != nil {
		return Asset{}, err
	}
	if strings.TrimSpace(string(output)) == "[]" || strings.Contains(string(output), `"changes()":0`) {
		return Asset{}, ErrNotFound
	}
	return r.Get(ctx, id)
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	output, err := r.query(ctx, fmt.Sprintf("PRAGMA foreign_keys = ON; DELETE FROM assets WHERE id = %s; SELECT changes();", quote(id)))
	if err != nil {
		return err
	}
	if strings.Contains(string(output), `"changes()":0`) || strings.TrimSpace(string(output)) == "[]" {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) variants(ctx context.Context, id string) ([]Variant, error) {
	output, err := r.query(ctx, fmt.Sprintf(`SELECT width, height, format, object_key AS key, url, size FROM variants
WHERE asset_id = %s ORDER BY width, CASE format WHEN 'avif' THEN 1 WHEN 'webp' THEN 2 ELSE 3 END;`, quote(id)))
	if err != nil {
		return nil, err
	}
	variants := []Variant{}
	if len(output) > 0 {
		err = json.Unmarshal(output, &variants)
	}
	return variants, err
}

type assetRow struct {
	ID, Filename, MIMEType, Checksum           string
	Width, Height                              int
	Alt, Caption, TakenAt, Location, Copyright string
	Tags, ArticleRefs, CreatedAt, UpdatedAt    string
}

func (row assetRow) asset() Asset {
	asset := Asset{ID: row.ID, Filename: row.Filename, MIMEType: row.MIMEType, Checksum: row.Checksum,
		Width: row.Width, Height: row.Height, Alt: row.Alt, Caption: row.Caption, TakenAt: row.TakenAt,
		Location: row.Location, Copyright: row.Copyright, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		Tags: []string{}, ArticleRefs: []string{}}
	_ = json.Unmarshal([]byte(row.Tags), &asset.Tags)
	_ = json.Unmarshal([]byte(row.ArticleRefs), &asset.ArticleRefs)
	return asset
}

func (r *Repository) query(ctx context.Context, statement string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	command := exec.CommandContext(ctx, "sqlite3", "-json", r.path, statement)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("sqlite query: %s", strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (r *Repository) run(ctx context.Context, statement string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	command := exec.CommandContext(ctx, "sqlite3", "-bail", r.path)
	command.Stdin = strings.NewReader(statement)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("sqlite command: %s", strings.TrimSpace(string(output)))
	}
	return output, nil
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func normalizeList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
