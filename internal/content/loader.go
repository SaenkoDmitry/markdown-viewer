package content

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

func Load(dir string, sessionPrefix string) (map[string]*Page, []NavItem, error) {
	pages := make(map[string]*Page)
	var nav []NavItem

	tempPages := make(map[string]bool)
	tempImages := make(map[string]string)

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		rel, _ := filepath.Rel(dir, path)

		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".md" {
			slug := strings.TrimSuffix(rel, ".md")
			urlPath := Slugify(slug)
			tempPages[urlPath] = true
		} else if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp" || ext == ".svg" {
			base := filepath.Base(rel)
			tempImages[base] = rel
			tempImages[rel] = rel
		}

		return nil
	})

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		if filepath.Ext(path) != ".md" {
			return nil
		}

		rel, _ := filepath.Rel(dir, path)
		slug := strings.TrimSuffix(rel, ".md")
		urlPath := Slugify(slug)
		title := filepath.Base(slug)

		rawContent, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		processedContent := ProcessWikiImages(string(rawContent), tempImages, sessionPrefix)
		processedContent = ProcessWikiLinks(processedContent, tempPages, sessionPrefix)

		pages[urlPath] = &Page{
			Title:   title,
			Content: template.HTML(MDToHTML([]byte(processedContent))),
			Path:    urlPath,
		}

		nav = append(nav, NavItem{
			Title:    title,
			Path:     sessionPrefix + urlPath,
			Level:    0,
			IsFolder: false,
		})
		return nil
	})
	return pages, nav, err
}

func FindMarkdownDir(root string) string {
	for _, sub := range []string{"content", "docs", "notes", "wiki"} {
		path := filepath.Join(root, sub)
		if hasMarkdown(path) {
			return path
		}
	}
	if hasMarkdown(root) {
		return root
	}
	return ""
}

func hasMarkdown(dir string) bool {
	found := false
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(path) == ".md" {
			found = true
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

func FindIndexPage(pages map[string]*Page) string {
	candidates := []string{"readme", "index", "база-знаний", "new-база-знаний"}

	for _, c := range candidates {
		for k := range pages {
			if k == c || strings.Contains(k, c) {
				return k
			}
		}
	}

	var first string
	for k := range pages {
		if first == "" || k < first {
			first = k
		}
	}
	return first
}
