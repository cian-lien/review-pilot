package contracts

import "regexp"

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{16,}|AKIA[A-Z0-9]{16})`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|secret|password|access[_-]?token)\s*[:=]\s*["']?)[^\s"';,]{8,}`),
}

func RedactReview(text string) string {
	text = secretPatterns[0].ReplaceAllString(text, "[REDACTED]")
	return secretPatterns[1].ReplaceAllString(text, "${1}[REDACTED]")
}
