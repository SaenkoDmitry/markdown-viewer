package content

import (
	"fmt"
	"html/template"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

type Page struct {
	Title      string
	Content    template.HTML
	Path       string
	Sidebar    []NavItem
	SessionID  string
	RepoURL    string
	Branch     string
	PagesCount int
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

// PageTitleFromPath извлекает название страницы из пути файла (без расширения)
func PageTitleFromPath(filePath string) string {
	base := filepath.Base(filePath)
	ext := filepath.Ext(base)
	title := strings.TrimSuffix(base, ext)
	// Заменяем дефисы и подчёркивания на пробелы для читаемости
	title = strings.ReplaceAll(title, "-", " ")
	title = strings.ReplaceAll(title, "_", " ")
	return title
}

// PrependTitle добавляет h1-заголовок с названием файла в начало markdown-контента
func PrependTitle(md []byte, filePath string) []byte {
	title := PageTitleFromPath(filePath)
	if title == "" {
		return md
	}
	// Формируем h1 заголовок и добавляем к контенту
	header := fmt.Sprintf("# %s\n\n", title)
	return append([]byte(header), md...)
}

// ProcessCheckboxes преобразует HTML-чекбоксы из markdown в стилизованные
// Работает с готовым HTML после парсинга markdown
func ProcessCheckboxes(html string) string {
	// Markdown парсер превращает `- [x] текст` в:
	// <li>[x] текст</li>  или  <p>- [x] текст</p>
	// Нам нужно найти [x] и [ ] внутри <li> и <p> и заменить на HTML-чекбоксы

	// Сначала обрабатываем элементы списка <li>
	liCheckboxRegex := regexp.MustCompile(`(?i)(<li[^>]*>)(?:[\s]*)?\[([ xX])\](?:[\s]+)(.*?)(</li>)`)
	html = liCheckboxRegex.ReplaceAllStringFunc(html, func(match string) string {
		groups := liCheckboxRegex.FindStringSubmatch(match)
		if groups == nil {
			return match
		}

		openTag := groups[1]
		checked := strings.ToLower(groups[2]) == "x"
		text := groups[3]
		closeTag := groups[4]

		checkbox := `<input type="checkbox" disabled`
		if checked {
			checkbox += ` checked`
		}
		checkbox += `>`

		if checked {
			return openTag + checkbox + ` <s>` + text + `</s>` + closeTag
		}
		return openTag + checkbox + ` ` + text + closeTag
	})

	// Обрабатываем параграфы <p> с чекбоксами (inline списки)
	pCheckboxRegex := regexp.MustCompile(`(?i)(<p[^>]*>.*?)(?:[\s]*)?\[([ xX])\](?:[\s]+)([^<]*)(.*?</p>)`)
	html = pCheckboxRegex.ReplaceAllStringFunc(html, func(match string) string {
		groups := pCheckboxRegex.FindStringSubmatch(match)
		if groups == nil {
			return match
		}

		prefix := groups[1]
		checked := strings.ToLower(groups[2]) == "x"
		text := groups[3]
		suffix := groups[4]

		checkbox := `<input type="checkbox" disabled`
		if checked {
			checkbox += ` checked`
		}
		checkbox += `>`

		if checked {
			return prefix + checkbox + ` <s>` + text + `</s>` + suffix
		}
		return prefix + checkbox + ` ` + text + suffix
	})

	return html
}

// tableRenderHook добавляет CSS-классы к таблицам для стилизации границ
func tableRenderHook(w io.Writer, node ast.Node, entering bool) (ast.WalkStatus, bool) {
	switch n := node.(type) {
	case *ast.Table:
		if entering {
			io.WriteString(w, `<div class="table-wrapper"><table class="md-table">`)
		} else {
			io.WriteString(w, "</table></div>\n")
		}
		return ast.GoToNext, true
	case *ast.TableHeader:
		if entering {
			io.WriteString(w, `<thead class="md-table-head">`)
		} else {
			io.WriteString(w, "</thead>\n")
		}
		return ast.GoToNext, true
	case *ast.TableBody:
		if entering {
			io.WriteString(w, `<tbody class="md-table-body">`)
		} else {
			io.WriteString(w, "</tbody>\n")
		}
		return ast.GoToNext, true
	case *ast.TableRow:
		if entering {
			io.WriteString(w, `<tr class="md-table-row">`)
		} else {
			io.WriteString(w, "</tr>\n")
		}
		return ast.GoToNext, true
	case *ast.TableCell:
		if entering {
			if n.IsHeader {
				io.WriteString(w, `<th class="md-table-header-cell"`)
			} else {
				io.WriteString(w, `<td class="md-table-cell"`)
			}
			// Добавляем выравнивание если есть
			switch n.Align {
			case ast.TableAlignmentLeft:
				io.WriteString(w, ` style="text-align:left"`)
			case ast.TableAlignmentRight:
				io.WriteString(w, ` style="text-align:right"`)
			case ast.TableAlignmentCenter:
				io.WriteString(w, ` style="text-align:center"`)
			}
			io.WriteString(w, ">")
		} else {
			if n.IsHeader {
				io.WriteString(w, "</th>")
			} else {
				io.WriteString(w, "</td>")
			}
		}
		return ast.GoToNext, true
	}
	return ast.GoToNext, false
}

// isListItem проверяет, является ли строка элементом списка
func isListItem(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
		return true
	}
	matched, _ := regexp.MatchString(`^\d+\.\s`, trimmed)
	return matched
}

// isHeading проверяет, является ли строка markdown-заголовком
func isHeading(line string) bool {
	trimmed := strings.TrimSpace(line)
	matched, _ := regexp.MatchString(`^#{1,6}\s`, trimmed)
	return matched
}

// FixListToHeadingSpacing добавляет пустую строку между элементами списка
// и следующими за ними заголовками. Без этого CommonMark парсит заголовок
// как вложенный контент элемента списка, что даёт лишний отступ.
func FixListToHeadingSpacing(md []byte) []byte {
	lines := strings.Split(string(md), "\n")
	var result []string

	for i := 0; i < len(lines); i++ {
		result = append(result, lines[i])

		if isListItem(lines[i]) {
			// Ищем следующую непустую строку
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			// Если следующая непустая строка — заголовок и между ними нет пустой строки
			if j < len(lines) && isHeading(lines[j]) && j == i+1 {
				result = append(result, "")
			}
		}
	}

	return []byte(strings.Join(result, "\n"))
}

func MDToHTML(md []byte) []byte {
	md = FixListToHeadingSpacing(md)

	extensions := parser.CommonExtensions | parser.AutoHeadingIDs
	p := parser.NewWithExtensions(extensions)
	doc := p.Parse(md)

	htmlFlags := html.CommonFlags | html.HrefTargetBlank
	opts := html.RendererOptions{
		Flags:          htmlFlags,
		RenderNodeHook: tableRenderHook,
	}
	renderer := html.NewRenderer(opts)

	htmlBytes := markdown.Render(doc, renderer)

	htmlStr := ProcessCheckboxes(string(htmlBytes))

	return []byte(htmlStr)
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
			return fmt.Sprintf(`<img src="%s" alt="%s" width="s" style="max-width:100%%;height:auto;">`, src, alt, width)
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
