package manager

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ContentItem is a language-independent Hugo content reference exposed to the
// media manager. Titles retain their language variants for display purposes.
type ContentItem struct {
	Ref       string            `json:"ref"`
	Section   string            `json:"section"`
	Title     string            `json:"title"`
	Languages []string          `json:"languages"`
	Titles    map[string]string `json:"titles"`
}

func listHugoContent(root, query string) ([]ContentItem, error) {
	if strings.TrimSpace(root) == "" {
		return []ContentItem{}, nil
	}
	items := make(map[string]*ContentItem)
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
		base, language := contentBaseAndLanguage(parts[len(parts)-1])
		if base == "_index" {
			return nil
		}
		title, draft, err := readFrontMatter(path)
		if err != nil {
			return err
		}
		if draft || title == "" {
			return nil
		}
		refParts := append([]string(nil), parts[:len(parts)-1]...)
		if base != "index" {
			refParts = append(refParts, base)
		}
		ref := strings.Trim(strings.Join(refParts, "/"), "/")
		if ref == parts[0] {
			return nil
		}
		item := items[ref]
		if item == nil {
			item = &ContentItem{Ref: ref, Section: parts[0], Titles: make(map[string]string)}
			items[ref] = item
		}
		item.Titles[language] = title
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return []ContentItem{}, nil
	}
	if err != nil {
		return nil, err
	}

	needle := strings.ToLower(strings.TrimSpace(query))
	result := make([]ContentItem, 0, len(items))
	for _, item := range items {
		item.Languages = make([]string, 0, len(item.Titles))
		matches := needle == "" || strings.Contains(strings.ToLower(item.Ref), needle)
		for language, title := range item.Titles {
			item.Languages = append(item.Languages, language)
			matches = matches || strings.Contains(strings.ToLower(title), needle)
		}
		sort.Strings(item.Languages)
		item.Title = preferredTitle(item.Titles, item.Languages)
		if matches {
			result = append(result, *item)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Section == result[j].Section {
			return result[i].Ref < result[j].Ref
		}
		return result[i].Section < result[j].Section
	})
	return result, nil
}

func contentBaseAndLanguage(filename string) (string, string) {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	for _, language := range []string{"en", "zh"} {
		suffix := "." + language
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix), language
		}
	}
	return name, "default"
}

func readFrontMatter(path string) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return "", false, scanner.Err()
	}
	var title string
	var draft bool
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			return title, draft, nil
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "title":
			title = frontMatterString(value)
		case "draft":
			draft = strings.EqualFold(strings.TrimSpace(value), "true")
		}
	}
	if err := scanner.Err(); err != nil {
		return "", false, err
	}
	return "", false, errors.New("unterminated YAML front matter: " + path)
}

func frontMatterString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return strings.TrimSpace(unquoted)
		}
	}
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		value = value[1 : len(value)-1]
	}
	return strings.TrimSpace(value)
}

func preferredTitle(titles map[string]string, languages []string) string {
	for _, language := range []string{"zh", "en", "default"} {
		if title := titles[language]; title != "" {
			return title
		}
	}
	if len(languages) > 0 {
		return titles[languages[0]]
	}
	return ""
}
