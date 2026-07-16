package reader

import "os"

func readMarkdown(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}
