package util

import "github.com/atotto/clipboard"

// Copy writes text to the system clipboard. Returns an error if unavailable.
func Copy(text string) error {
	return clipboard.WriteAll(text)
}

// Paste reads text from the system clipboard.
func Paste() (string, error) {
	return clipboard.ReadAll()
}
