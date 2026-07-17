package main

import (
	"context"
	"fmt"
	"image"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v3"
	ui "github.com/metaspartan/gotui/v5"
	"github.com/metaspartan/gotui/v5/widgets"

	"ai-adv-agent/internal/app"
)

// tuiState holds the three widgets of the orchestrator TUI (header, scrollable
// conversation log, single-line input) and the backing log lines.
type tuiState struct {
	header *widgets.Paragraph
	log    *widgets.List
	input  *widgets.TextArea // multi-line: Enter = newline, Ctrl+S = submit
	lines  []string

	// raw keeps every log line as received (unwrapped, unstyled). The log widget
	// runs with WrapText=false and we wrap lines ourselves (see appendRendered):
	// gotui's List scrolls by moving SelectedRow between LOGICAL rows and only
	// shifts its window when the selection leaves it, so with WrapText=true long
	// wrapped rows (consultant answers) made PgUp/wheel move an invisible
	// selection inside the window while the screen stayed still. Pre-wrapping
	// keeps one logical row per screen row: scrolling and bottom-follow are exact.
	// raw is the source for re-wrapping on terminal resize.
	raw   []string
	wrapW int // current wrap width (log inner width)

	// follow keeps the log pinned to the newest line. It is released when the
	// user scrolls up (wheel / PgUp) so they can read earlier output while a task
	// keeps streaming, and re-armed once they scroll back to the bottom.
	follow bool

	// Input history: submitted lines, browsable with ↑/↓. histPos indexes into
	// history; histPos == len(history) means "the live draft" (draft holds the
	// unsent text so browsing away and back does not lose it).
	history []string
	histPos int
	draft   string

	// inCode tracks whether the log is currently inside a ``` fenced code block,
	// so those lines render with a gutter and escaped (verbatim) content.
	inCode bool

	// awaiting is non-nil while the orchestrator is blocked waiting for the user
	// to approve/comment on a plan: the next submitted line is sent here (the
	// reply channel) instead of being treated as a new task.
	awaiting chan string

	// status is the one-line run summary shown in the header; refreshHeader
	// rebuilds the header text around it (the key hints depend on the mode).
	status string

	// selecting is the mouse text-selection mode. It is OFF by default: the mouse
	// stays captured so touchpad/wheel scrolling of the log works out of the box —
	// diagnostics showed that with the mouse released the terminal delivers NO
	// scroll events at all (it scrolls its own buffer instead), so capture is the
	// only way to get touchpad scrolling. Text can still be selected while
	// captured via the terminal's own modifier (Option+drag in iTerm2, Fn+drag in
	// Terminal.app). Toggling selection ON (/select or F2) releases the mouse for
	// native no-modifier selection at the cost of touchpad scrolling; keyboard
	// scrolling (PgUp/PgDn/Home/End) always works. (History: capture-default →
	// selection-default → capture-default again; selection-default lost touchpad
	// scroll entirely, and the modifier-drag escape hatch was the missing piece.)
	selecting bool

	// lastAnswer is the most recent finished answer, used by /copy.
	lastAnswer string

	// consultant is non-nil while the session is in documentation-consultant
	// mode (/help enters, /end leaves): input lines are answered by the grounded
	// docs Q&A instead of being dispatched as orchestrator tasks.
	consultant *app.Consultant

	// keyDebug (toggled by /keys) logs every keyboard event — the gotui event ID
	// plus the raw tcell key/modifiers/name — so a user whose submit combo never
	// arrives can see exactly what their terminal delivers and report it.
	keyDebug bool
}

// logKeyEvent appends one diagnostic line for a keyboard event. The gotui ID is
// what our switch matches on; the tcell payload shows the raw key, modifier bits
// and tcell's own name for the combo.
func (s *tuiState) logKeyEvent(e ui.Event) {
	line := fmt.Sprintf("[key] id=%q", e.ID)
	if ek, ok := e.Payload.(*tcell.EventKey); ok && ek != nil {
		line += fmt.Sprintf("  tcell: key=%d mods=%04b name=%q", ek.Key(), ek.Modifiers(), ek.Name())
	}
	s.appendLog(line)
}

// inputTitle returns the input box title for the current mode.
func (s *tuiState) inputTitle() string {
	if s.consultant != nil {
		return "Консультант по документации (Ctrl+S — отправить, /end — выход из режима)"
	}
	return "Ввод (Ctrl+S — отправить, Enter — новая строка)"
}

// refreshHeader rebuilds the header text: the status line plus the key hints for
// the current mode.
func (s *tuiState) refreshHeader() {
	// Default (mouse captured): touchpad/wheel scrolls the log; selection needs
	// the terminal's modifier-drag or the /select mode.
	hint := "тачпад/колесо — прокрутка · выделение: Option+drag (iTerm2) / Fn+drag (Terminal.app) или /select · Ctrl+S — отправить · Enter — новая строка · Ctrl+C — выход"
	if s.selecting {
		hint = "РЕЖИМ ВЫДЕЛЕНИЯ: выделяйте мышью без модификаторов · тачпад-скролл выкл (PgUp/PgDn работают) · /select — вернуть прокрутку"
	}
	s.header.Text = s.status + "\n" + hint
}

// toggleSelect flips the mouse text-selection mode, releasing or re-grabbing the
// terminal's mouse so native selection can be used to copy log text.
func (s *tuiState) toggleSelect() {
	s.selecting = !s.selecting
	if scr := ui.DefaultBackend.Screen; scr != nil {
		if s.selecting {
			scr.DisableMouse()
		} else {
			scr.EnableMouse()
		}
	}
	s.refreshHeader()
}

// copyLastAnswer copies the last finished answer to the system clipboard and
// appends a status line to the log.
func (s *tuiState) copyLastAnswer() {
	if strings.TrimSpace(s.lastAnswer) == "" {
		s.appendLog("Нет ответа для копирования.")
		return
	}
	method, err := copyToClipboard(s.lastAnswer)
	if err != nil {
		s.appendLog("⚠ Копирование не удалось: " + err.Error())
		return
	}
	s.appendLog(fmt.Sprintf("Скопировано в буфер обмена (%s): %d симв.", method, utf8.RuneCountInString(s.lastAnswer)))
}

// askReq carries a plan-approval request from the orchestrator's goroutine to the
// TUI event loop; reply delivers the user's response back.
type askReq struct {
	prompt string
	reply  chan string
}

// tuiPrompter implements app.UserPrompter by handing the request to the event
// loop over askCh and blocking on the reply (or context cancellation).
type tuiPrompter struct{ askCh chan askReq }

func (p tuiPrompter) AskUser(ctx context.Context, prompt string) (string, error) {
	reply := make(chan string, 1)
	select {
	case p.askCh <- askReq{prompt: prompt, reply: reply}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	select {
	case r := <-reply:
		return r, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// appendLog adds text (possibly multi-line) to the log. Each line is wrapped to
// the log width and colourised (see appendRendered). It auto-scrolls to the
// bottom only while following, so a user who scrolled up to read earlier output
// is not yanked back down by streaming progress.
func (s *tuiState) appendLog(text string) {
	for _, ln := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		s.raw = append(s.raw, ln)
		s.appendRendered(ln)
	}
	s.log.Rows = s.lines
	if s.follow && len(s.lines) > 0 {
		s.log.SelectedRow = len(s.lines) - 1
	}
}

// appendRendered wraps one raw line to the log width and appends the styled
// screen rows. The first chunk keeps the full renderRow treatment (fence
// toggling, tags, headings, bullets); continuation chunks are styled inline-only
// so a wrap boundary can never fake a fence, bullet or tag prefix. Code-block
// lines wrap 2 columns narrower to leave room for the gutter.
func (s *tuiState) appendRendered(ln string) {
	trimmed := strings.TrimSpace(ln)
	if strings.HasPrefix(trimmed, "```") {
		s.lines = append(s.lines, s.renderRow(ln)) // fence marker: toggles inCode
		return
	}
	if s.inCode {
		for _, chunk := range wrapRunes(ln, s.wrapW-2) {
			s.lines = append(s.lines, styledTag("│ ", styleCodeGutter)+escMarkup(chunk))
		}
		return
	}
	for i, chunk := range wrapRunes(ln, s.wrapW) {
		if i == 0 {
			s.lines = append(s.lines, s.renderRow(chunk))
		} else {
			s.lines = append(s.lines, mdInline(chunk))
		}
	}
}

// wrapRunes splits s into rune-aware chunks of at most w runes. A non-positive
// width (start-up, tiny terminal) disables wrapping.
func wrapRunes(s string, w int) []string {
	if w <= 0 || utf8.RuneCountInString(s) <= w {
		return []string{s}
	}
	runes := []rune(s)
	var out []string
	for len(runes) > w {
		out = append(out, string(runes[:w]))
		runes = runes[w:]
	}
	return append(out, string(runes))
}

// rebuildLog re-wraps the whole log from the raw lines (after a resize changed
// the wrap width), preserving the fence state machine and the follow position.
func (s *tuiState) rebuildLog() {
	s.lines = nil
	s.inCode = false
	for _, ln := range s.raw {
		s.appendRendered(ln)
	}
	s.log.Rows = s.lines
	if s.follow {
		s.toBottom()
	} else if s.log.SelectedRow >= len(s.lines) {
		s.log.SelectedRow = len(s.lines) - 1
	}
}

// scroll moves the visible window by delta rows (negative = up), clamped. It
// releases follow-mode; reaching the bottom re-arms it.
func (s *tuiState) scroll(delta int) {
	s.follow = false
	last := len(s.lines) - 1
	if last < 0 {
		last = 0
	}
	s.log.SelectedRow += delta
	if s.log.SelectedRow < 0 {
		s.log.SelectedRow = 0
	}
	if s.log.SelectedRow >= last {
		s.log.SelectedRow = last
		s.follow = true // back at the bottom → resume auto-follow
	}
}

// toBottom jumps to the newest line and re-arms follow; toTop jumps to the start.
func (s *tuiState) toBottom() {
	s.follow = true
	if n := len(s.lines); n > 0 {
		s.log.SelectedRow = n - 1
	}
}

func (s *tuiState) toTop() {
	s.follow = false
	s.log.SelectedRow = 0
}

// setInput replaces the input text and puts the cursor at its very end (rune- and
// line-aware, since the input is a multi-line text area).
func (s *tuiState) setInput(text string) {
	s.input.Text = text
	lines := strings.Split(text, "\n")
	y := len(lines) - 1
	s.input.Cursor = image.Point{X: utf8.RuneCountInString(lines[y]), Y: y}
}

// clearInput empties the input and resets the cursor to the top-left.
func (s *tuiState) clearInput() {
	s.input.Text = ""
	s.input.Cursor = image.Point{}
}

// pushHistory records a submitted line (skipping a consecutive duplicate) and
// resets the browse position to the live draft.
func (s *tuiState) pushHistory(line string) {
	if n := len(s.history); n == 0 || s.history[n-1] != line {
		s.history = append(s.history, line)
	}
	s.histPos = len(s.history)
	s.draft = ""
}

// historyPrev recalls an older input into the line (↑).
func (s *tuiState) historyPrev() {
	if len(s.history) == 0 {
		return
	}
	if s.histPos == len(s.history) {
		s.draft = s.input.Text // preserve the unsent draft before browsing
	}
	if s.histPos > 0 {
		s.histPos--
	}
	s.setInput(s.history[s.histPos])
}

// historyNext recalls a newer input, or restores the draft at the end (↓).
func (s *tuiState) historyNext() {
	if s.histPos >= len(s.history) {
		return
	}
	s.histPos++
	if s.histPos == len(s.history) {
		s.setInput(s.draft)
	} else {
		s.setInput(s.history[s.histPos])
	}
}

// layout positions the widgets for the current terminal size.
func (s *tuiState) layout() {
	w, h := ui.TerminalDimensions()
	if w < 20 {
		w = 80
	}
	if h < 8 {
		h = 24
	}
	// The input is a multi-line box (~4 visible rows); it scrolls internally when
	// the text grows taller.
	inputH := 6
	if h-inputH < 5 {
		inputH = 3
	}
	s.header.SetRect(0, 0, w, 3)
	s.log.SetRect(0, 3, w, h-inputH)
	s.input.SetRect(0, h-inputH, w, h)
	s.wrapW = w - 2 // log inner width (minus the borders) for our own wrapping
}

func (s *tuiState) render() { ui.Render(s.header, s.log, s.input) }

// tuiWriter is an io.Writer that forwards the orchestrator's progress output
// into the TUI event loop over a channel, so spawn/route logs land in the log
// widget instead of corrupting the screen.
type tuiWriter struct{ ch chan<- string }

func (w tuiWriter) Write(p []byte) (int, error) {
	w.ch <- string(p)
	return len(p), nil
}

// runOrchestratorTUI runs the interactive orchestrator inside a full-screen
// gotui interface: a header, a scrollable conversation log, and an input line.
// Slash commands are handled locally (never sent to the model); a task runs in a
// background goroutine while progress streams into the log, keeping the UI
// responsive. /help enters the documentation-consultant mode (grounded Q&A over
// docs.db + git MCP tools), /end leaves it. Ctrl+C or /exit quits.
func runOrchestratorTUI(orch *app.Orchestrator, tb *app.Toolbelt, status string) error {
	if err := ui.Init(); err != nil {
		return fmt.Errorf("init TUI: %w", err)
	}
	defer ui.Close()

	st := &tuiState{
		header: widgets.NewParagraph(),
		log:    widgets.NewList(),
		input:  widgets.NewTextArea(),
		follow: true,
		// selecting stays false: ui.Init() grabs the mouse and we keep it, so
		// touchpad/wheel scrolling works immediately (see the selecting field
		// comment for the tradeoff and history).
	}
	st.header.Title = "Оркестратор (Day31)"
	st.status = status
	st.refreshHeader()
	st.header.WrapText = false
	st.log.Title = "Диалог"
	st.log.WrapText = false // wrapping is ours (appendRendered) — exact scrolling
	st.input.Title = st.inputTitle()
	st.layout()
	st.appendLog("Интерактивный оркестратор. /help — консультант по документации и список команд.")
	st.render()
	defer func() {
		if st.consultant != nil {
			st.consultant.Close()
		}
	}()

	// Route orchestrator + sub-agent progress into the log via a channel.
	progressCh := make(chan string, 256)
	orch.SetOutput(tuiWriter{ch: progressCh})

	// Enable the plan-approval gate: the orchestrator hands approval requests to
	// this loop, which shows them and feeds the user's reply back.
	askCh := make(chan askReq)
	orch.SetPrompter(tuiPrompter{askCh: askCh})

	type doneMsg struct {
		answer string
		err    error
	}
	doneCh := make(chan doneMsg, 1)
	busy := false

	uiEvents := ui.PollEvents()
	for {
		select {
		case e := <-uiEvents:
			// Key diagnostics (/keys): show what the terminal actually delivers
			// before normal handling, so a dead key combo can be identified.
			if st.keyDebug && e.Type == ui.KeyboardEvent {
				st.logKeyEvent(e)
				st.render()
			}
			switch e.ID {
			case "<C-c>":
				return nil

			case "<Resize>":
				oldW := st.wrapW
				st.layout()
				if st.wrapW != oldW {
					st.rebuildLog() // re-wrap the log for the new width
				}
				ui.Clear()
				st.render()

			case "<PageUp>":
				st.scroll(-10)
				st.render()
			case "<PageDown>":
				st.scroll(10)
				st.render()
			case "<MouseWheelUp>":
				st.scroll(-3)
				st.render()
			case "<MouseWheelDown>":
				st.scroll(3)
				st.render()
			case "<Home>":
				st.toTop()
				st.render()
			case "<End>":
				st.toBottom()
				st.render()
			case "<F2>":
				st.toggleSelect()
				st.render()

			case "<Up>":
				// At the top line, ↑ recalls previous input; otherwise it moves the
				// cursor up within the multi-line text.
				if st.input.Cursor.Y == 0 {
					st.historyPrev()
				} else {
					st.input.MoveCursor(0, -1)
				}
				st.render()
			case "<Down>":
				// At the bottom line, ↓ recalls newer input; otherwise cursor down.
				if st.input.Cursor.Y >= strings.Count(st.input.Text, "\n") {
					st.historyNext()
				} else {
					st.input.MoveCursor(0, 1)
				}
				st.render()

			case "<Backspace>":
				st.input.DeleteRune()
				st.render()
			case "<Left>":
				st.input.MoveCursor(-1, 0)
				st.render()
			case "<Right>":
				st.input.MoveCursor(1, 0)
				st.render()
			case "<Space>":
				st.input.InsertRune(' ')
				st.render()

			case "<Enter>":
				// Enter inserts a newline so multi-line / multi-paragraph input (typed
				// or pasted — gotui delivers a pasted \n as an Enter event, so paste
				// lands intact) is composed in place; Ctrl+S submits it.
				st.input.InsertNewline()
				st.render()

			case "<C-s>":
				// Submit. Ctrl+letter combos have proven fragile on macOS (Ctrl+D
				// hit a window-manager shortcut; Ctrl+S reportedly never arrives on
				// one setup) — /keys toggles key diagnostics so a dead combo can be
				// identified from what the terminal actually delivers.
				// While awaiting plan approval, the text is the user's reply and is
				// sent back to the blocked orchestrator instead of starting a task.
				if st.awaiting != nil {
					reply := strings.TrimSpace(st.input.Text)
					st.clearInput()
					st.appendLog("› " + reply)
					ch := st.awaiting
					st.awaiting = nil
					st.input.Title = st.inputTitle()
					st.render()
					ch <- reply
					break
				}
				if busy {
					break // ignore submit while a task is running
				}
				line := strings.TrimSpace(st.input.Text)
				st.clearInput()
				if line == "" {
					st.render()
					break
				}
				st.pushHistory(line)
				st.toBottom() // a fresh submission re-arms follow so the answer shows
				st.appendLog("› " + line)
				switch classifyInput(line, orchestratorCommands) {
				case cmdQuit:
					return nil
				case cmdHelp:
					st.appendLog(renderCommandHelp(orchestratorCommands))
					if st.consultant != nil {
						st.appendLog("Вы уже в режиме консультанта по документации. Выход — /end.")
						break
					}
					c, err := tb.NewConsultant()
					if err != nil {
						st.appendLog("⚠ Режим консультанта недоступен: " + err.Error())
						break
					}
					c.SetOutput(tuiWriter{ch: progressCh})
					st.consultant = c
					st.input.Title = st.inputTitle()
					st.appendLog(c.Intro())
				case cmdEnd:
					if st.consultant == nil {
						st.appendLog("Вы не в режиме консультанта. /help — войти в него.")
						break
					}
					st.consultant.Close()
					st.consultant = nil
					st.input.Title = st.inputTitle()
					st.appendLog("Режим консультанта завершён — снова оркестратор задач.")
				case cmdAgents:
					st.appendLog(orch.AgentsSummary())
				case cmdMemory:
					st.appendLog(orch.MemorySummary())
				case cmdMCP:
					st.appendLog(orch.MCPSummary())
				case cmdTools:
					st.appendLog(orch.ToolsSummary())
				case cmdCopy:
					st.copyLastAnswer()
				case cmdSelect:
					st.toggleSelect()
				case cmdKeys:
					st.keyDebug = !st.keyDebug
					if st.keyDebug {
						st.appendLog("Диагностика клавиш ВКЛ: каждое нажатие пишется в лог. Нажмите вашу комбинацию отправки и посмотрите её id. /keys — выключить.")
					} else {
						st.appendLog("Диагностика клавиш выключена.")
					}
				case cmdClear:
					st.lines = nil
					st.raw = nil
					st.log.Rows = nil
					st.log.SelectedRow = 0
					st.inCode = false
					st.follow = true
				case cmdUnknown:
					st.appendLog(fmt.Sprintf("Неизвестная команда %q. /help — список команд.", strings.Fields(line)[0]))
				default: // cmdInput → run the consultant or the orchestrator in the background
					busy = true
					st.appendLog("… обрабатываю …")
					consultant := st.consultant // snapshot: mode can't change while busy
					go func(task string) {
						ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
						defer cancel()
						var ans string
						var err error
						if consultant != nil {
							ans, err = consultant.Ask(ctx, task)
						} else {
							ans, err = orch.Handle(ctx, task)
						}
						doneCh <- doneMsg{answer: ans, err: err}
					}(line)
				}
				st.render()

			default:
				// A single printable rune (works for Cyrillic — count runes, not
				// bytes, and skip bracketed special-key IDs like "<Up>").
				if !strings.HasPrefix(e.ID, "<") && utf8.RuneCountInString(e.ID) == 1 {
					st.input.InsertRune([]rune(e.ID)[0])
					st.render()
				}
			}

		case req := <-askCh:
			// The orchestrator paused for plan approval: show it and switch the
			// input line into reply mode. busy stays true — Handle is blocked in
			// AskUser until the user submits a reply (handled in <Enter> above).
			st.awaiting = req.reply
			st.toBottom()
			st.appendLog(req.prompt)
			st.input.Title = "Ответ (согласуйте / прокомментируйте / верните на доработку)"
			st.render()

		case line := <-progressCh:
			st.appendLog(strings.TrimRight(line, "\n"))
			st.render()

		case d := <-doneCh:
			busy = false
			if d.err != nil {
				st.appendLog("⚠ Ошибка: " + d.err.Error())
			} else {
				st.lastAnswer = d.answer
				st.appendLog(d.answer)
				st.appendLog("(/copy — скопировать ответ · выделение: Option/Fn+drag или /select)")
			}
			st.render()
		}
	}
}
