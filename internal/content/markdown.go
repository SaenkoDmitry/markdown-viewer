package content

import (
	"fmt"
	"html/template"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

type Page struct {
	Title   string
	Content template.HTML
	Path    string
	Sidebar []NavItem
}

type NavItem struct {
	Title    string
	Path     string
	Level    int
	IsFolder bool
}

func Slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")

	var b strings.Builder
	for _, r := range s {
		isLatin := r >= 'a' && r <= 'z'
		isCyrillic := (r >= 'а' && r <= 'я') || r == 'ё'
		isDigit := r >= '0' && r <= '9'
		isDash := r == '-'

		if isLatin || isCyrillic || isDigit || isDash {
			b.WriteRune(r)
		}
	}
	s = b.String()

	re := regexp.MustCompile(`-+`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")

	return s
}

func MDToHTML(md []byte) []byte {
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs
	p := parser.NewWithExtensions(extensions)
	doc := p.Parse(md)

	htmlFlags := html.CommonFlags | html.HrefTargetBlank
	opts := html.RendererOptions{Flags: htmlFlags}
	renderer := html.NewRenderer(opts)

	return markdown.Render(doc, renderer)
}

func ProcessWikiLinks(content string, allPages map[string]bool, sessionPrefix string) string {
	wikiLinkRegex := regexp.MustCompile(`\[\[([^\]|#]+)(?:#([^\]|]+))?(?:\|([^\]]+))?\]\]`)

	return wikiLinkRegex.ReplaceAllStringFunc(content, func(match string) string {
		groups := wikiLinkRegex.FindStringSubmatch(match)
		if groups == nil {
			return match
		}

		target := strings.TrimSpace(groups[1])
		heading := groups[2]
		text := groups[3]

		linkText := text
		if linkText == "" {
			linkText = target
			if heading != "" {
				linkText = heading
			}
		}

		urlTarget := Slugify(target)
		href := sessionPrefix + urlTarget
		if heading != "" {
			anchor := Slugify(heading)
			href += "#" + anchor
		}

		return `[` + linkText + `](` + href + `)`
	})
}

// ProcessWikiImages обрабатывает:
// ![[image.png]] → стандартный markdown
// ![[image.png|300]] → <img src="..." width="300">
// ![[image.png|описание|300]] → <img src="..." alt="описание" width="300">
func ProcessWikiImages(content string, images map[string]string, sessionPrefix string) string {
	// ![[filename]] или ![[filename|alt]] или ![[filename|alt|width]] или ![[filename|width]]
	wikiImageRegex := regexp.MustCompile(`!\[\[([^\]|]+)(?:\|([^\]|]+))?(?:\|([^\]]+))?\]\]`)

	return wikiImageRegex.ReplaceAllStringFunc(content, func(match string) string {
		groups := wikiImageRegex.FindStringSubmatch(match)
		if groups == nil {
			return match
		}

		filename := strings.TrimSpace(groups[1])
		param1 := strings.TrimSpace(groups[2]) // может быть alt или width
		param2 := strings.TrimSpace(groups[3]) // может быть width (если param1 — alt)

		// Ищем картинку
		imgPath, ok := images[filename]
		if !ok {
			for k, v := range images {
				if filepath.Base(k) == filename {
					imgPath = v
					break
				}
			}
		}

		if imgPath == "" {
			return `![` + filename + `](` + filename + `)`
		}

		imgPath = strings.ReplaceAll(imgPath, "\\", "/")
		src := sessionPrefix + "raw/" + imgPath

		// Определяем alt и width
		var alt, width string

		if param2 != "" {
			// Формат: ![[file|alt|width]]
			alt = param1
			width = param2
		} else if param1 != "" {
			// Либо alt, либо width
			if isNumeric(param1) {
				width = param1
				alt = strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
			} else {
				alt = param1
			}
		} else {
			alt = strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
		}

		// Если width указан — используем HTML img, иначе стандартный markdown
		if width != "" {
			return fmt.Sprintf(`<img src="%s" alt="%s" width="%s" style="max-width:100%%;height:auto;">`, src, alt, width)
		}

		return `![` + alt + `](` + src + `)`
	})
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
