package wiki

import (
	"fmt"
	"regexp"
	"strings"
)

// Quality rejection reasons. All of them are recorded in the manifest, so a
// human reviewing a dry run can tell "we chose not to index this" from "we never
// saw this".
const (
	ReasonEmpty    = "empty"
	ReasonTOCOnly  = "toc_only"
	ReasonRedirect = "redirect"
	ReasonDraft    = "draft"
	ReasonArchived = "archived"
)

// minContentRunes is the floor below which a page carries no answer worth
// retrieving. A stub of a few words only adds noise to the index.
const minContentRunes = 40

var (
	// A redirect stub: a page whose whole body points elsewhere.
	// No \b here: Go's word boundary is ASCII-only, so it never matches after a
	// Cyrillic letter and would silently disable every Russian alternative.
	reRedirect = regexp.MustCompile(`(?i)^\s*(#redirect|см\.\s+страниц|см\.\s+статью|see\s+page|перенесено\s+в|переехало\s+в|moved\s+to|redirect\s+to)`)
	// Draft/archived markers, as a leading banner line.
	reDraft    = regexp.MustCompile(`(?im)^\s*(#{0,6}\s*)?(\[?(черновик|draft|wip|work in progress)\]?)\s*:?\s*$`)
	reArchived = regexp.MustCompile(`(?im)^\s*(#{0,6}\s*)?(\[?(архив|архивная|archived|deprecated|устарел[оа]?)\]?)\s*:?\s*$`)

	// A markdown link line, optionally bulleted/numbered — the building block of
	// a table-of-contents page.
	reLinkLine = regexp.MustCompile(`^\s*(?:[-*+]\s+|\d+[.)]\s+)?\[[^\]]*\]\([^)]*\)\s*$`)
	// A markdown image: ![alt](src)
	reImage = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]*)[^)]*\)`)
	// An HTML img tag, which wiki exports often emit instead of markdown.
	reIMGTag = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	reIMGSrc = regexp.MustCompile(`(?i)\b(?:alt|src)\s*=\s*"([^"]*)"`)
	// An attachment macro some wikis emit.
	reAttachment = regexp.MustCompile(`(?i)\{\{\s*(?:attachment|file|вложение)\s*:\s*([^}]+)\}\}`)
)

// CheckQuality decides whether a page's content is worth indexing.
// ok=false means skip, with reason naming why.
func CheckQuality(title, body string) (ok bool, reason string) {
	text := strings.TrimSpace(body)
	if text == "" {
		return false, ReasonEmpty
	}

	if reRedirect.MatchString(text) {
		return false, ReasonRedirect
	}
	if reDraft.MatchString(text) || isMarkedTitle(title, "черновик", "draft", "wip") {
		return false, ReasonDraft
	}
	if reArchived.MatchString(text) || isMarkedTitle(title, "архив", "archived", "deprecated", "устарел") {
		return false, ReasonArchived
	}

	if isTOCOnly(text) {
		return false, ReasonTOCOnly
	}

	// The length test runs last, on the prose that survives after headings and
	// links are discounted: a page that is only a heading plus two links is not
	// rescued by the characters in those links.
	if len([]rune(proseOnly(text))) < minContentRunes {
		return false, ReasonEmpty
	}

	return true, ""
}

// isMarkedTitle reports whether the title is prefixed/bracketed with a marker
// like "[Архив] Old process".
func isMarkedTitle(title string, markers ...string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	for _, m := range markers {
		if strings.HasPrefix(t, m) ||
			strings.HasPrefix(t, "["+m) ||
			strings.HasPrefix(t, "("+m) {
			return true
		}
	}
	return false
}

// isTOCOnly reports whether a page is nothing but a list of links to other
// pages. Such a page answers no question on its own, and its links are
// retrieved as their own pages anyway.
func isTOCOnly(text string) bool {
	var links, prose int
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if reLinkLine.MatchString(l) {
			links++
			continue
		}
		prose++
	}
	return links >= 3 && prose == 0
}

// proseOnly strips headings and link markup so the length test measures actual
// prose rather than navigation furniture.
func proseOnly(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") || reLinkLine.MatchString(l) {
			continue
		}
		b.WriteString(l)
		b.WriteString(" ")
	}
	return strings.TrimSpace(b.String())
}

// NormalizeContent prepares a page body for chunking.
//
// Attachments and images are NOT downloaded — that would mean fetching binary
// blobs over OAuth, storing them somewhere, and doing nothing useful with them,
// since the embedder is text-only. Instead each one is replaced by a
// "[изображение: <name>]" placeholder, which keeps the surrounding prose
// readable and tells a reader that something visual belongs there.
func NormalizeContent(body string) string {
	s := strings.ReplaceAll(body, "\r\n", "\n")

	s = reImage.ReplaceAllStringFunc(s, func(m string) string {
		g := reImage.FindStringSubmatch(m)
		return imagePlaceholder(firstNonEmpty(g[1], baseName(g[2])))
	})
	s = reIMGTag.ReplaceAllStringFunc(s, func(m string) string {
		var name string
		for _, g := range reIMGSrc.FindAllStringSubmatch(m, -1) {
			if name = strings.TrimSpace(g[1]); name != "" {
				break
			}
		}
		return imagePlaceholder(baseName(name))
	})
	s = reAttachment.ReplaceAllStringFunc(s, func(m string) string {
		g := reAttachment.FindStringSubmatch(m)
		return imagePlaceholder(baseName(strings.TrimSpace(g[1])))
	})

	s = normalizeTables(s)

	// Collapse runs of blank lines left behind by the replacements.
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

func imagePlaceholder(name string) string {
	if name == "" {
		return "[изображение]"
	}
	return fmt.Sprintf("[изображение: %s]", name)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// baseName reduces a path or URL to its last segment, dropping any query string.
func baseName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimRight(s, "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// normalizeTables converts HTML tables to markdown, KEEPING THE HEADER ROW.
// The header is what makes a table answerable: without "Тариф | Срок | Цена"
// above the numbers, a retrieved row is a meaningless tuple.
func normalizeTables(s string) string {
	if !strings.Contains(strings.ToLower(s), "<table") {
		return s
	}
	return reTable.ReplaceAllStringFunc(s, func(tbl string) string {
		rows := reRow.FindAllString(tbl, -1)
		if len(rows) == 0 {
			return ""
		}

		var out []string
		headerDone := false
		for _, row := range rows {
			cells := cellsOf(row)
			if len(cells) == 0 {
				continue
			}
			out = append(out, "| "+strings.Join(cells, " | ")+" |")
			if !headerDone {
				// The separator row is what makes the first row a header in
				// markdown; emit it right after the first row we keep.
				sep := make([]string, len(cells))
				for i := range sep {
					sep[i] = "---"
				}
				out = append(out, "| "+strings.Join(sep, " | ")+" |")
				headerDone = true
			}
		}
		if len(out) == 0 {
			return ""
		}
		return "\n" + strings.Join(out, "\n") + "\n"
	})
}

var (
	reTable = regexp.MustCompile(`(?is)<table\b.*?</table>`)
	reRow   = regexp.MustCompile(`(?is)<tr\b.*?</tr>`)
	reCell  = regexp.MustCompile(`(?is)<(th|td)\b[^>]*>(.*?)</(?:th|td)>`)
	reTag   = regexp.MustCompile(`(?is)<[^>]+>`)
)

// cellsOf extracts the text of one table row's cells, flattening nested markup.
func cellsOf(row string) []string {
	matches := reCell.FindAllStringSubmatch(row, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		text := reTag.ReplaceAllString(m[2], " ")
		text = strings.Join(strings.Fields(text), " ")
		// A pipe inside a cell would break the markdown row apart.
		text = strings.ReplaceAll(text, "|", "\\|")
		out = append(out, text)
	}
	return out
}
