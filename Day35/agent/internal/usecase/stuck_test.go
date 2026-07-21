package usecase

import (
	"errors"
	"strings"
	"testing"
)

// Regression: a sub-agent fed paths from the wrong root called read_text_file with
// arguments the guard could never accept and burned every round on that one dead
// end. The detector must cut the loop short instead.
func TestStuckDetector(t *testing.T) {
	guardErr := errors.New("BAD_ARGUMENT: путь \"Day34/workspace/bot.py\" не существует относительно корня Day34/")
	otherPath := errors.New("BAD_ARGUMENT: путь \"Day34/agent/main.go\" не существует относительно корня Day34/")

	t.Run("same tool and code trips regardless of the path", func(t *testing.T) {
		var s stuckDetector
		for i := 0; i < maxRepeatedToolFailures-1; i++ {
			s.record("read_text_file", guardErr)
			if _, halted := s.verdict(); halted {
				t.Fatalf("halted early after %d failures", i+1)
			}
		}
		// A different path but the same failure is still the same dead end.
		s.record("read_text_file", otherPath)
		msg, halted := s.verdict()
		if !halted {
			t.Fatal("must halt once the identical failure repeats")
		}
		if !strings.Contains(msg, "read_text_file") {
			t.Errorf("message must name the tool, got: %s", msg)
		}
		if !strings.Contains(msg, "корня") {
			t.Errorf("message must carry the last error, got: %s", msg)
		}
	})

	t.Run("a success resets the counter", func(t *testing.T) {
		var s stuckDetector
		for i := 0; i < maxRepeatedToolFailures-1; i++ {
			s.record("read_text_file", guardErr)
		}
		s.reset()
		s.record("read_text_file", guardErr)
		if _, halted := s.verdict(); halted {
			t.Fatal("a successful call must clear the streak")
		}
	})

	t.Run("a different failure resets the counter", func(t *testing.T) {
		var s stuckDetector
		for i := 0; i < maxRepeatedToolFailures-1; i++ {
			s.record("read_text_file", guardErr)
		}
		s.record("read_text_file", errors.New("READ_ONLY_PATH: запись запрещена"))
		if _, halted := s.verdict(); halted {
			t.Fatal("a new kind of failure means the model is still exploring")
		}
	})

	t.Run("a different tool resets the counter", func(t *testing.T) {
		var s stuckDetector
		for i := 0; i < maxRepeatedToolFailures-1; i++ {
			s.record("read_text_file", guardErr)
		}
		s.record("list_directory", guardErr)
		if _, halted := s.verdict(); halted {
			t.Fatal("switching tools means progress is still possible")
		}
	})
}

func TestFailureKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"BAD_ARGUMENT: путь не существует", "BAD_ARGUMENT"},
		{"READ_ONLY_PATH: запись запрещена", "READ_ONLY_PATH"},
		{"unknown tool \"x\"", "unknown tool \"x\""},
	}
	for _, c := range cases {
		if got := failureKey(errors.New(c.in)); got != c.want {
			t.Errorf("failureKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
