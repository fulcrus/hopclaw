package toolruntime

import (
	"fmt"
	"strings"
)

// computeDiff generates a unified diff between two texts.
// Returns an empty string if the texts are identical.
func computeDiff(oldName, newName, oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	oldLines := splitLines(oldText)
	newLines := splitLines(newText)
	edits := diffLines(oldLines, newLines)
	if len(edits) == 0 {
		return ""
	}
	hunks := groupHunks(edits, 3)
	if len(hunks) == 0 {
		return ""
	}
	return formatUnified(oldName, newName, hunks)
}

// editEntry is a single line in the edit script.
type editEntry struct {
	kind byte   // ' ' context, '-' delete, '+' insert
	text string // line content without trailing newline
}

// diffLines computes the shortest edit script between old and new line slices
// using the Myers diff algorithm.
func diffLines(oldLines, newLines []string) []editEntry {
	n := len(oldLines)
	m := len(newLines)

	if n == 0 && m == 0 {
		return nil
	}
	if n == 0 {
		edits := make([]editEntry, m)
		for i, line := range newLines {
			edits[i] = editEntry{kind: '+', text: line}
		}
		return edits
	}
	if m == 0 {
		edits := make([]editEntry, n)
		for i, line := range oldLines {
			edits[i] = editEntry{kind: '-', text: line}
		}
		return edits
	}

	// Myers diff: find shortest edit script.
	max := n + m
	size := 2*max + 1
	v := make([]int, size)
	trace := make([][]int, 0, max+1)

	for d := 0; d <= max; d++ {
		snap := make([]int, size)
		copy(snap, v)
		trace = append(trace, snap)

		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[k-1+max] < v[k+1+max]) {
				x = v[k+1+max] // insert: move down
			} else {
				x = v[k-1+max] + 1 // delete: move right
			}
			y := x - k
			for x < n && y < m && oldLines[x] == newLines[y] {
				x++
				y++
			}
			v[k+max] = x
			if x >= n && y >= m {
				return backtrackEdits(trace, oldLines, newLines, d, max)
			}
		}
	}
	return nil
}

// backtrackEdits reconstructs the edit script from the Myers trace.
func backtrackEdits(trace [][]int, oldLines, newLines []string, d, max int) []editEntry {
	x, y := len(oldLines), len(newLines)
	edits := make([]editEntry, 0, len(oldLines)+len(newLines))

	for dd := d; dd > 0; dd-- {
		v := trace[dd] // v at start of step dd = v after step dd-1
		k := x - y

		var prevK int
		if k == -dd || (k != dd && v[k-1+max] < v[k+1+max]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[prevK+max]
		prevY := prevX - prevK

		// Diagonal: equal lines (in reverse).
		for x > prevX && y > prevY {
			x--
			y--
			edits = append(edits, editEntry{kind: ' ', text: oldLines[x]})
		}

		if x == prevX {
			y--
			edits = append(edits, editEntry{kind: '+', text: newLines[y]})
		} else {
			x--
			edits = append(edits, editEntry{kind: '-', text: oldLines[x]})
		}
	}

	// Remaining diagonal at the start.
	for x > 0 && y > 0 {
		x--
		y--
		edits = append(edits, editEntry{kind: ' ', text: oldLines[x]})
	}

	// Reverse to forward order.
	for i, j := 0, len(edits)-1; i < j; i, j = i+1, j-1 {
		edits[i], edits[j] = edits[j], edits[i]
	}
	return edits
}

// diffHunk is a unified diff hunk.
type diffHunk struct {
	oldStart int // 1-based
	oldCount int
	newStart int // 1-based
	newCount int
	lines    []editEntry
}

// groupHunks groups edit entries into hunks with the given number of context lines.
func groupHunks(edits []editEntry, contextSize int) []diffHunk {
	// Find change regions (contiguous non-context edits).
	type region struct{ start, end int }
	var regions []region
	for i := 0; i < len(edits); {
		if edits[i].kind != ' ' {
			start := i
			for i < len(edits) && edits[i].kind != ' ' {
				i++
			}
			regions = append(regions, region{start, i})
		} else {
			i++
		}
	}
	if len(regions) == 0 {
		return nil
	}

	// Merge regions that are close (gap <= 2*contextSize).
	type group struct{ regions []region }
	groups := []group{{regions: []region{regions[0]}}}
	for i := 1; i < len(regions); i++ {
		gap := regions[i].start - regions[i-1].end
		if gap <= 2*contextSize {
			groups[len(groups)-1].regions = append(groups[len(groups)-1].regions, regions[i])
		} else {
			groups = append(groups, group{regions: []region{regions[i]}})
		}
	}

	// Build hunks.
	hunks := make([]diffHunk, 0, len(groups))
	for _, g := range groups {
		first := g.regions[0]
		last := g.regions[len(g.regions)-1]

		lo := first.start - contextSize
		if lo < 0 {
			lo = 0
		}
		hi := last.end + contextSize
		if hi > len(edits) {
			hi = len(edits)
		}

		lines := make([]editEntry, hi-lo)
		copy(lines, edits[lo:hi])

		// Compute old/new line numbers.
		oldLine, newLine := 1, 1
		for i := 0; i < lo; i++ {
			switch edits[i].kind {
			case ' ':
				oldLine++
				newLine++
			case '-':
				oldLine++
			case '+':
				newLine++
			}
		}

		oldCount, newCount := 0, 0
		for _, e := range lines {
			switch e.kind {
			case ' ':
				oldCount++
				newCount++
			case '-':
				oldCount++
			case '+':
				newCount++
			}
		}

		hunks = append(hunks, diffHunk{
			oldStart: oldLine,
			oldCount: oldCount,
			newStart: newLine,
			newCount: newCount,
			lines:    lines,
		})
	}
	return hunks
}

// formatUnified renders hunks as a unified diff string.
func formatUnified(oldName, newName string, hunks []diffHunk) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n", oldName)
	fmt.Fprintf(&b, "+++ %s\n", newName)
	for _, h := range hunks {
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", h.oldStart, h.oldCount, h.newStart, h.newCount)
		for _, line := range h.lines {
			b.WriteByte(line.kind)
			b.WriteString(line.text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// splitLines splits text into lines, stripping trailing newlines from each.
// An empty string produces an empty slice.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// Remove trailing empty element from trailing newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
