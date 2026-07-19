package main

import "strings"

// This file colourises the TUI conversation log. gotui's List renders each row
// through ui.ParseStyles, which interprets a "[text](fg:colour,mod:bold)" inline
// markup — great for highlighting stages, but it silently mangles ordinary text
// that happens to contain the same tokens (e.g. Go code like arr[i](y) or a
// slice s[1:len(s)]). renderRow therefore either builds *safe* markup from
// content we control, or escapes untrusted content so the parser prints it
// verbatim (used for the LLM answer and fenced code blocks).

// zwsp is a zero-width space: invisible, but enough to break the parser's
// "]" → "(" / "]" → "[" adjacency so escaped text is never consumed as markup.
const zwsp = "​"

// escMarkup makes ui.ParseStyles render s verbatim. The parser only ever alters
// text via a balanced "[...]" followed by "(" or "[", so inserting a zero-width
// space after every "]" (plus a trailing one to flush any bracket left open at
// end-of-line) neutralises every case while changing nothing visible.
func escMarkup(s string) string {
	if !strings.ContainsAny(s, "[]") {
		return s
	}
	return strings.ReplaceAll(s, "]", "]"+zwsp) + zwsp
}

// styledTag wraps bracket/paren-free text in a colour style. If text contains a
// markup token it can't be wrapped safely, so it is escaped (rendered uncoloured)
// instead — correctness over colour.
func styledTag(text, style string) string {
	if strings.ContainsAny(text, "[]()") {
		return escMarkup(text)
	}
	return "[" + text + "](" + style + ")"
}

// styledBracketTag renders a "[label]" (brackets kept) in a colour. It relies on
// the parser's nesting: the inner "[label]" is balanced styled text.
func styledBracketTag(label, style string) string {
	if strings.ContainsAny(label, "[]()") {
		return escMarkup("[" + label + "]")
	}
	return "[[" + label + "]](" + style + ")"
}

// Style specs (gotui markup) for the log's line categories.
const (
	styleUser       = "fg:cyan,mod:bold"
	styleOrch       = "fg:cyan,mod:bold"
	styleSubagent   = "fg:violet,mod:bold"
	styleTool       = "fg:teal"
	styleErr        = "fg:red,mod:bold"
	styleHeader     = "fg:yellow,mod:bold"
	styleBullet     = "fg:green"
	styleBold       = "mod:bold"
	styleInlineCode = "fg:teal"
	styleCodeFence  = "fg:darkgrey"
	styleCodeGutter = "fg:teal"
)

// renderRow turns one raw log line into a markup-safe, colourised row and tracks
// the fenced-code-block state so code renders verbatim behind a coloured gutter.
func (s *tuiState) renderRow(raw string) string {
	trimmed := strings.TrimSpace(raw)

	// ``` toggles a fenced code block; show the fence as a dim separator.
	if strings.HasPrefix(trimmed, "```") {
		s.inCode = !s.inCode
		return styledTag("──────── код ────────", styleCodeFence)
	}
	if s.inCode {
		return styledTag("│ ", styleCodeGutter) + escMarkup(raw)
	}

	switch {
	case strings.HasPrefix(raw, "› "):
		return styledTag("› ", styleUser) + mdInline(strings.TrimPrefix(raw, "› "))
	case strings.HasPrefix(raw, "⚠"):
		return styledTag("⚠ ", styleErr) + escMarkup(strings.TrimSpace(strings.TrimPrefix(raw, "⚠")))
	case strings.HasPrefix(raw, "[orchestrator]"):
		return styledBracketTag("orchestrator", styleOrch) + escMarkup(strings.TrimPrefix(raw, "[orchestrator]"))
	case strings.HasPrefix(raw, "[subagent"):
		return colorBracketPrefix(raw, styleSubagent)
	case strings.HasPrefix(raw, "[rag]"):
		return styledBracketTag("rag", styleTool) + escMarkup(strings.TrimPrefix(raw, "[rag]"))
	case strings.HasPrefix(raw, "[mcp]"):
		return styledBracketTag("mcp", styleTool) + escMarkup(strings.TrimPrefix(raw, "[mcp]"))
	case strings.HasPrefix(raw, "==="):
		return styledTag(raw, styleHeader)
	case strings.HasPrefix(raw, "  • "):
		return styledTag("  • ", styleBullet) + mdInline(raw[len("  • "):])
	}

	// Markdown-lite prose formatting (plans and answers come as markdown).
	if h, ok := mdHeading(trimmed); ok {
		return styledTag(h, styleHeader)
	}
	if mdRule(trimmed) {
		return styledTag("────────────────────", styleCodeFence)
	}
	if marker, rest, ok := mdListItem(raw); ok {
		return styledTag(marker, styleBullet) + mdInline(rest)
	}
	return mdInline(raw)
}

// mdHeading matches a markdown ATX heading ("# ..." up to "###### ...") and
// returns the heading text with the hashes stripped.
func mdHeading(trimmed string) (string, bool) {
	i := 0
	for i < len(trimmed) && i < 6 && trimmed[i] == '#' {
		i++
	}
	if i == 0 || i >= len(trimmed) || trimmed[i] != ' ' {
		return "", false
	}
	return strings.TrimSpace(trimmed[i+1:]), true
}

// mdRule matches a markdown horizontal rule ("---", "***", "___").
func mdRule(trimmed string) bool {
	if len(trimmed) < 3 {
		return false
	}
	c := trimmed[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	return strings.Trim(trimmed, string(c)) == ""
}

// mdListItem detects a bullet ("- ", "+ ") or ordered ("N. ", "N) ") list item,
// preserving leading indent, and returns a normalised marker plus the item text.
// Bullets are unified to "• "; ordered items keep their number. "* " is
// deliberately NOT treated as a bullet — it collides with unfenced code (pointer
// deref, multiplication, globs like "*.go"), so such lines render verbatim.
func mdListItem(raw string) (marker, rest string, ok bool) {
	t := strings.TrimLeft(raw, " ")
	indent := raw[:len(raw)-len(t)]
	if len(t) >= 2 && (t[0] == '-' || t[0] == '+') && t[1] == ' ' {
		return indent + "• ", t[2:], true
	}
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(t) && (t[i] == '.' || t[i] == ')') && t[i+1] == ' ' {
		return indent + t[:i+1] + " ", t[i+2:], true
	}
	return "", "", false
}

// mdInline renders inline markdown (`**bold**` and `` `code` ``) as safe gotui
// markup and escapes everything else so ordinary text (brackets, code) survives
// the parser verbatim. Byte indexing is safe: the only delimiters are ASCII, so
// multibyte (Cyrillic) runes are never split.
func mdInline(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		switch {
		case strings.HasPrefix(s[i:], "**"):
			// Emphasis only when the ** are not intraword (flanking rule): this keeps
			// code expressions like a**b**c, 2**8, or 3**2 * 4**5 intact instead of
			// eating the run between two ** as bold.
			if i == 0 || !isWordByte(s[i-1]) {
				if end := strings.Index(s[i+2:], "**"); end > 0 {
					after := i + 2 + end + 2
					if after >= len(s) || !isWordByte(s[after]) {
						b.WriteString(styledTag(s[i+2:i+2+end], styleBold))
						i = after
						continue
					}
				}
			}
		case s[i] == '`':
			if end := strings.IndexByte(s[i+1:], '`'); end > 0 {
				b.WriteString(styledTag(s[i+1:i+1+end], styleInlineCode))
				i += 1 + end + 1
				continue
			}
		}
		// Accumulate plain text up to the next inline delimiter, then escape it.
		j := i + 1
		for j < len(s) && s[j] != '`' && !strings.HasPrefix(s[j:], "**") {
			j++
		}
		b.WriteString(escMarkup(s[i:j]))
		i = j
	}
	return b.String()
}

// isWordByte reports whether b is a "word" byte for the emphasis flanking check:
// ASCII letters/digits/underscore, or any UTF-8 continuation/lead byte (>=0x80),
// so a ** adjacent to a Latin or Cyrillic word is treated as intraword.
func isWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '_', b >= 0x80:
		return true
	}
	return false
}

// colorBracketPrefix colours a leading "[label] rest" line (e.g. the sub-agent
// tag "[subagent researcher] …"), keeping the brackets and escaping the rest.
func colorBracketPrefix(raw, style string) string {
	idx := strings.IndexByte(raw, ']')
	if idx < 0 {
		return escMarkup(raw)
	}
	label := raw[1:idx]
	return styledBracketTag(label, style) + escMarkup(raw[idx+1:])
}
