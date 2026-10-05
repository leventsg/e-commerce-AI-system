package evolution

import (
	"fmt"
	"strings"
)

// unifiedDiff produces a minimal unified diff between two texts. It is used only
// for the candidate skill body versus the embedded original; "add" is reported
// as a new file instead of a diff.
func unifiedDiff(oldText, newText string) string {
	oldLines := splitLines(oldText)
	newLines := splitLines(newText)
	lcs := lcsTable(oldLines, newLines)

	var out []string
	i, j := len(oldLines), len(newLines)
	var hunks []string
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			i--
			j--
			continue
		}
		if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			hunks = append([]string{"+ " + newLines[j-1]}, hunks...)
			j--
		} else {
			hunks = append([]string{"- " + oldLines[i-1]}, hunks...)
			i--
		}
	}
	out = append(out, "--- a/SKILL.md", "+++ b/SKILL.md", "@@ skill body @@")
	out = append(out, hunks...)
	return strings.Join(out, "\n")
}

func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func lcsTable(a, b []string) [][]int {
	table := make([][]int, len(a)+1)
	for i := range table {
		table[i] = make([]int, len(b)+1)
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				table[i][j] = table[i-1][j-1] + 1
			} else if table[i-1][j] >= table[i][j-1] {
				table[i][j] = table[i-1][j]
			} else {
				table[i][j] = table[i][j-1]
			}
		}
	}
	return table
}

// skillDiff returns the candidate-versus-baseline diff text.
func skillDiff(operation, skillName, baseline, candidate string) string {
	if operation == "add" {
		return fmt.Sprintf("# 新增 Skill %s（无原版本，视为新增文件）\n\n%s", skillName, candidate)
	}
	return unifiedDiff(baseline, candidate)
}
