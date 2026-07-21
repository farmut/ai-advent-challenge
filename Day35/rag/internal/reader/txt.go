package reader

import "os"

func readTxt(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}
