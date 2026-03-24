package format

// Preview returns a truncated string for logging
func Preview(s string, length int) string {
	if len(s) <= length {
		return s
	}
	return s[:length] + "..."
}
