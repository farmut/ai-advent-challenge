package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// copyToClipboard puts text on the system clipboard. It first tries a native CLI
// tool for the platform (pbcopy on macOS, wl-copy / xclip / xsel on Linux); if
// none is available it falls back to an OSC 52 terminal escape, which many
// terminals honour and which also works over SSH. Returns a short human-readable
// description of the method used, or an error if nothing worked.
func copyToClipboard(text string) (method string, err error) {
	if tool, args := clipboardTool(); tool != "" {
		cmd := exec.Command(tool, args...)
		cmd.Stdin = strings.NewReader(text)
		if e := cmd.Run(); e == nil {
			return tool, nil
		} else {
			err = e
		}
	}
	// Fallback: OSC 52. Write straight to the terminal (stdout is the tty even
	// under the tcell alt-screen); the emulator consumes the sequence without
	// touching the screen grid.
	if e := writeOSC52(text); e == nil {
		return "OSC52", nil
	} else if err == nil {
		err = e
	}
	if err == nil {
		err = fmt.Errorf("нет доступного механизма буфера обмена")
	}
	return "", err
}

// clipboardTool returns the clipboard CLI and its args for the current OS, or an
// empty string when none is known.
func clipboardTool() (string, []string) {
	if runtime.GOOS == "darwin" {
		if p, err := exec.LookPath("pbcopy"); err == nil {
			return p, nil
		}
		return "", nil
	}
	// Linux / other: prefer Wayland, then X11 tools.
	for _, c := range []struct {
		name string
		args []string
	}{
		{"wl-copy", nil},
		{"xclip", []string{"-selection", "clipboard"}},
		{"xsel", []string{"--clipboard", "--input"}},
	} {
		if p, err := exec.LookPath(c.name); err == nil {
			return p, c.args
		}
	}
	return "", nil
}

// writeOSC52 emits the clipboard-set escape sequence for text.
func writeOSC52(text string) error {
	enc := base64.StdEncoding.EncodeToString([]byte(text))
	_, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\a", enc)
	return err
}
