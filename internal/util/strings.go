package util

import "strings"

// Truncate shortens s to at most n runes, appending "…" if truncated.
// Operates on runes, not bytes, to avoid breaking UTF-8 multi-byte characters.
func Truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "\u2026"
}

// SanitizeFilename produces a filename safe across Linux/macOS/Windows.
// Conservative: alphanumerics, dash, underscore, dot. Everything else → "-".
// Trims leading/trailing dashes. Truncates to 100 chars.
func SanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "document"
	}
	if len(out) > 100 {
		out = out[:100]
	}
	return out
}
