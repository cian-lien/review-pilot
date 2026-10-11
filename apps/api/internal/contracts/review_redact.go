package contracts

import (
	"encoding/json"
	"regexp"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{16,}|AKIA[A-Z0-9]{16})`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|secret|password|access[_-]?token)\s*[:=]\s*["']?)[^\s"';,]{8,}`),
}

func RedactReview(text string) string {
	text = secretPatterns[0].ReplaceAllString(text, "[REDACTED]")
	return secretPatterns[1].ReplaceAllString(text, "${1}[REDACTED]")
}
func RedactReviewJSON(body []byte) []byte {
	var value any
	if json.Unmarshal(body, &value) != nil {
		return body
	}
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case string:
			return RedactReview(x)
		case []any:
			for i, c := range x {
				x[i] = walk(c)
			}
		case map[string]any:
			for k, c := range x {
				x[k] = walk(c)
			}
		}
		return v
	}
	output, err := json.Marshal(walk(value))
	if err != nil {
		return body
	}
	return output
}
