package content

import (
	"html/template"
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
