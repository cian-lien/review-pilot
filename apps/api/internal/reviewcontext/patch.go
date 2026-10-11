package reviewcontext

import (
	"regexp"
	"strconv"
	"strings"
)

var hunkPattern = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@.*$`)

func codeLines(text string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// MatchPatch proves textual anchors only, not symbol resolution or runtime behavior.
func MatchPatch(patch, base, head string, additions, deletions int) bool {
	oldLines, newLines := codeLines(base), codeLines(head)
	oldLine, newLine, oldLeft, newLeft, added, removed := 0, 0, 0, 0, 0, 0
	started := false
	for _, line := range codeLines(patch) {
		if match := hunkPattern.FindStringSubmatch(line); match != nil {
			if oldLeft != 0 || newLeft != 0 {
				return false
			}
			oldLine, _ = strconv.Atoi(match[1])
			newLine, _ = strconv.Atoi(match[3])
			oldLeft, newLeft = 1, 1
			if match[2] != "" {
				oldLeft, _ = strconv.Atoi(match[2])
			}
			if match[4] != "" {
				newLeft, _ = strconv.Atoi(match[4])
			}
			started = true
			continue
		}
		if line == `\ No newline at end of file` {
			continue
		}
		if !started || len(line) == 0 {
			return false
		}
		prefix := line[0]
		if prefix != ' ' && prefix != '+' && prefix != '-' {
			return false
		}
		if prefix == ' ' || prefix == '-' {
			if oldLeft <= 0 || oldLine <= 0 || oldLine > len(oldLines) || oldLines[oldLine-1] != line[1:] {
				return false
			}
			oldLine++
			oldLeft--
			if prefix == '-' {
				removed++
			}
		}
		if prefix == ' ' || prefix == '+' {
			if newLeft <= 0 || newLine <= 0 || newLine > len(newLines) || newLines[newLine-1] != line[1:] {
				return false
			}
			newLine++
			newLeft--
			if prefix == '+' {
				added++
			}
		}
	}
	return started && oldLeft == 0 && newLeft == 0 && added == additions && removed == deletions
}
