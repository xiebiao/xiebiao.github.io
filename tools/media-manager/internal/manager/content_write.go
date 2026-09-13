package manager

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type contentEdit struct {
	path     string
	original []byte
	updated  []byte
	mode     fs.FileMode
}

func (s *server) insertAssetIntoContent(asset Asset) error {
	if strings.TrimSpace(s.options.ContentRoot) == "" || len(asset.ArticleRefs) == 0 {
		return nil
	}
	s.contentMu.Lock()
	defer s.contentMu.Unlock()

	edits, err := prepareContentEdits(s.options.ContentRoot, asset)
	if err != nil {
		return err
	}
	return applyContentEdits(edits)
}

func prepareContentEdits(root string, asset Asset) ([]contentEdit, error) {
	var edits []contentEdit
	for _, ref := range normalizeList(asset.ArticleRefs) {
		section, _, ok := strings.Cut(strings.Trim(ref, "/"), "/")
		if !ok || (section != "posts" && section != "photos") {
			continue
		}
		paths, err := contentPathsForRef(root, ref)
		if err != nil {
			return nil, err
		}
		if len(paths) == 0 {
			return nil, fmt.Errorf("linked content %q was not found", ref)
		}
		for _, path := range paths {
			original, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			var updated []byte
			switch section {
			case "posts":
				updated = insertPostFigure(original, asset.ID)
			case "photos":
				updated, err = insertPhotoAsset(original, asset)
				if err != nil {
					return nil, fmt.Errorf("update %s: %w", path, err)
				}
			}
			if string(updated) == string(original) {
				continue
			}
			info, err := os.Stat(path)
			if err != nil {
				return nil, err
			}
			edits = append(edits, contentEdit{path: path, original: original, updated: updated, mode: info.Mode().Perm()})
		}
	}
	return edits, nil
}

func contentPathsForRef(root, wanted string) ([]string, error) {
	wanted = strings.Trim(strings.TrimSpace(wanted), "/")
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) < 2 || (parts[0] != "posts" && parts[0] != "photos") {
			return nil
		}
		base, _ := contentBaseAndLanguage(parts[len(parts)-1])
		if base == "_index" {
			return nil
		}
		refParts := append([]string(nil), parts[:len(parts)-1]...)
		if base != "index" {
			refParts = append(refParts, base)
		}
		if strings.Trim(strings.Join(refParts, "/"), "/") == wanted {
			paths = append(paths, path)
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	sort.Strings(paths)
	return paths, err
}

func insertPostFigure(content []byte, assetID string) []byte {
	shortcode := `{{< figure asset="` + assetID + `" >}}`
	if strings.Contains(string(content), shortcode) {
		return content
	}
	trimmed := strings.TrimRight(string(content), "\r\n")
	return []byte(trimmed + "\n\n" + shortcode + "\n")
}

func insertPhotoAsset(content []byte, asset Asset) ([]byte, error) {
	useCRLF := strings.Contains(string(content), "\r\n")
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	front, body, err := splitYAMLFrontMatter(normalized)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(front, "\n"), "\n")

	coverIndex := yamlKeyIndex(lines, "coverAsset")
	coverMatches := false
	if coverIndex >= 0 {
		_, value, _ := strings.Cut(lines[coverIndex], ":")
		coverMatches = frontMatterString(value) == asset.ID
		if frontMatterString(value) == "" {
			lines[coverIndex] = "coverAsset: " + strconv.Quote(asset.ID)
			coverMatches = true
		}
	} else {
		imagesIndex := yamlKeyIndex(lines, "images")
		if imagesIndex < 0 {
			imagesIndex = len(lines)
		}
		lines = insertLines(lines, imagesIndex, "coverAsset: "+strconv.Quote(asset.ID))
		coverMatches = true
	}
	if coverMatches {
		coverAltIndex := yamlKeyIndex(lines, "coverAlt")
		if coverAltIndex >= 0 {
			_, value, _ := strings.Cut(lines[coverAltIndex], ":")
			if frontMatterString(value) == "" {
				lines[coverAltIndex] = "coverAlt: " + strconv.Quote(asset.Alt)
			}
		} else {
			imagesIndex := yamlKeyIndex(lines, "images")
			if imagesIndex < 0 {
				imagesIndex = len(lines)
			}
			lines = insertLines(lines, imagesIndex, "coverAlt: "+strconv.Quote(asset.Alt))
		}
	}

	if !yamlImagesContainAsset(lines, asset.ID) {
		imagesIndex := yamlKeyIndex(lines, "images")
		if imagesIndex < 0 {
			lines = append(lines, "images:")
			imagesIndex = len(lines) - 1
		} else {
			_, value, _ := strings.Cut(lines[imagesIndex], ":")
			if strings.TrimSpace(value) == "[]" {
				lines[imagesIndex] = "images:"
			} else if strings.TrimSpace(value) != "" {
				return nil, errors.New("inline images front matter is not supported")
			}
		}
		insertAt := len(lines)
		for index := imagesIndex + 1; index < len(lines); index++ {
			if isTopLevelYAMLKey(lines[index]) {
				insertAt = index
				break
			}
		}
		item := []string{
			"  - asset: " + strconv.Quote(asset.ID),
			"    alt: " + strconv.Quote(asset.Alt),
			"    caption: " + strconv.Quote(asset.Caption),
		}
		lines = insertLines(lines, insertAt, item...)
	}

	updated := "---\n" + strings.Join(lines, "\n") + "\n---\n" + body
	if useCRLF {
		updated = strings.ReplaceAll(updated, "\n", "\r\n")
	}
	return []byte(updated), nil
}

func splitYAMLFrontMatter(content string) (string, string, error) {
	if !strings.HasPrefix(content, "---\n") {
		return "", "", errors.New("YAML front matter is required")
	}
	offset := len("---\n")
	for offset <= len(content) {
		lineEnd := strings.IndexByte(content[offset:], '\n')
		if lineEnd < 0 {
			lineEnd = len(content) - offset
		}
		line := content[offset : offset+lineEnd]
		next := offset + lineEnd
		if next < len(content) {
			next++
		}
		if strings.TrimSpace(line) == "---" {
			return content[len("---\n"):offset], content[next:], nil
		}
		if next <= offset {
			break
		}
		offset = next
	}
	return "", "", errors.New("unterminated YAML front matter")
}

func yamlKeyIndex(lines []string, wanted string) int {
	for index, line := range lines {
		if len(line) == 0 || line[0] == ' ' || line[0] == '\t' || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		key, _, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == wanted {
			return index
		}
	}
	return -1
}

func isTopLevelYAMLKey(line string) bool {
	if len(line) == 0 || line[0] == ' ' || line[0] == '\t' || strings.HasPrefix(strings.TrimSpace(line), "#") {
		return false
	}
	_, _, ok := strings.Cut(line, ":")
	return ok
}

func yamlImagesContainAsset(lines []string, assetID string) bool {
	imagesIndex := yamlKeyIndex(lines, "images")
	if imagesIndex < 0 {
		return false
	}
	for index := imagesIndex + 1; index < len(lines); index++ {
		if isTopLevelYAMLKey(lines[index]) {
			break
		}
		trimmed := strings.TrimSpace(lines[index])
		if !strings.HasPrefix(trimmed, "- asset:") {
			continue
		}
		_, value, _ := strings.Cut(trimmed, ":")
		if frontMatterString(value) == assetID {
			return true
		}
	}
	return false
}

func insertLines(lines []string, index int, additions ...string) []string {
	result := make([]string, 0, len(lines)+len(additions))
	result = append(result, lines[:index]...)
	result = append(result, additions...)
	result = append(result, lines[index:]...)
	return result
}

func applyContentEdits(edits []contentEdit) error {
	written := make([]contentEdit, 0, len(edits))
	for _, edit := range edits {
		if err := writeContentAtomically(edit.path, edit.updated, edit.mode); err != nil {
			for index := len(written) - 1; index >= 0; index-- {
				_ = writeContentAtomically(written[index].path, written[index].original, written[index].mode)
			}
			return err
		}
		written = append(written, edit)
	}
	return nil
}

func writeContentAtomically(path string, content []byte, mode fs.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".media-content-*.md")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
