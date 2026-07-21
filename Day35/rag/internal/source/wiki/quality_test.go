package wiki

import (
	"strings"
	"testing"
)

func TestCheckQualityRejections(t *testing.T) {
	tests := []struct {
		name       string
		title      string
		body       string
		wantOK     bool
		wantReason string
	}{
		{"empty", "T", "", false, ReasonEmpty},
		{"whitespace only", "T", "   \n\t\n  ", false, ReasonEmpty},
		{"too short", "T", "Коротко.", false, ReasonEmpty},
		{"redirect ru", "T", "См. страницу [Новая](/new)", false, ReasonRedirect},
		{"redirect en", "T", "#REDIRECT [[Other page]]", false, ReasonRedirect},
		{"moved to", "T", "Перенесено в раздел документации по отпускам и компенсациям", false, ReasonRedirect},
		{"draft banner", "T", "Черновик\n\nЗдесь будет описан процесс оформления отпуска сотрудника.", false, ReasonDraft},
		{"draft title", "[Черновик] Отпуска", longBody, false, ReasonDraft},
		{"archived banner", "T", "Архив\n\nСтарый процесс оформления отпуска, больше не применяется нигде.", false, ReasonArchived},
		{"archived title", "[Архив] Старый процесс", longBody, false, ReasonArchived},
		{"deprecated title", "Deprecated API guide", longBody, false, ReasonArchived},
		{"good page", "Отпуска", longBody, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := CheckQuality(tt.title, tt.body)
			if ok != tt.wantOK || reason != tt.wantReason {
				t.Errorf("CheckQuality() = (%t, %q), want (%t, %q)", ok, reason, tt.wantOK, tt.wantReason)
			}
		})
	}
}

// A page that is only links answers nothing on its own, and the pages it links
// to are indexed in their own right.
func TestCheckQualityTOCOnly(t *testing.T) {
	toc := `# Оглавление

- [Отпуска](/docs/vacation)
- [Больничные](/docs/sick)
- [Командировки](/docs/travel)
`
	if ok, reason := CheckQuality("Оглавление", toc); ok || reason != ReasonTOCOnly {
		t.Errorf("CheckQuality(toc) = (%t, %q), want (false, %q)", ok, reason, ReasonTOCOnly)
	}

	// The same links plus real prose is a real page.
	withProse := toc + "\nОтпуск оформляется за две недели до предполагаемой даты начала отдыха.\n"
	if ok, _ := CheckQuality("Оглавление", withProse); !ok {
		t.Error("a link list WITH prose was rejected as toc_only")
	}
}

// Attachments are not downloaded — the embedder is text-only, and fetching
// binaries over OAuth buys nothing. A placeholder keeps the prose readable.
func TestNormalizeContentImagePlaceholders(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"markdown with alt", "До ![схема процесса](/files/a.png) после", "[изображение: схема процесса]"},
		{"markdown without alt", "До ![](/files/diagram.png) после", "[изображение: diagram.png]"},
		{"html img alt", `<img alt="скриншот" src="/f/x.png">`, "[изображение: скриншот]"},
		{"html img src only", `<img src="/files/photo.jpg">`, "[изображение: photo.jpg]"},
		{"attachment macro", "{{attachment: отчёт.xlsx}}", "[изображение: отчёт.xlsx]"},
		{"query stripped", "![](/files/a.png?size=big)", "[изображение: a.png]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeContent(tt.in)
			if !strings.Contains(got, tt.want) {
				t.Errorf("NormalizeContent(%q) = %q, want it to contain %q", tt.in, got, tt.want)
			}
			if strings.Contains(got, "![") || strings.Contains(got, "<img") {
				t.Errorf("NormalizeContent(%q) = %q — raw image markup survived", tt.in, got)
			}
		})
	}
}

// A table without its header is a set of meaningless tuples: "30 | 14" answers
// nothing unless "Дней | Срок" is above it.
func TestNormalizeContentTableKeepsHeader(t *testing.T) {
	html := `<table>
		<tr><th>Тип отпуска</th><th>Дней</th></tr>
		<tr><td>Основной</td><td>28</td></tr>
		<tr><td>Дополнительный</td><td>7</td></tr>
	</table>`

	got := NormalizeContent(html)

	wantLines := []string{
		"| Тип отпуска | Дней |",
		"| --- | --- |",
		"| Основной | 28 |",
		"| Дополнительный | 7 |",
	}
	for _, w := range wantLines {
		if !strings.Contains(got, w) {
			t.Errorf("table markdown missing %q\n--- got ---\n%s", w, got)
		}
	}
	if strings.Contains(got, "<table") || strings.Contains(got, "<td") {
		t.Errorf("raw HTML survived:\n%s", got)
	}

	// The separator must come right after the header row, not later.
	if i, j := strings.Index(got, "| --- |"), strings.Index(got, "| Основной |"); i > j {
		t.Errorf("separator row is not directly after the header:\n%s", got)
	}
}

func TestNormalizeContentTableEscapesPipes(t *testing.T) {
	html := `<table><tr><th>A|B</th></tr><tr><td>x|y</td></tr></table>`
	got := NormalizeContent(html)

	if strings.Contains(got, "| A|B |") {
		t.Errorf("an unescaped pipe broke the row apart:\n%s", got)
	}
	if !strings.Contains(got, `A\|B`) {
		t.Errorf("pipe was not escaped:\n%s", got)
	}
}

func TestNormalizeContentCollapsesBlankLines(t *testing.T) {
	got := NormalizeContent("Абзац один.\n\n\n\n\nАбзац два.")
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("blank line runs survived: %q", got)
	}
}
