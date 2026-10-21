package check

import (
	"fmt"
	"strings"
)

const diffContext = 3

// maxDiffCells bounds the LCS table; larger inputs are shown as a single
// replace hunk.
const maxDiffCells = 16 << 20

// unifiedDiff returns a unified diff from a to b, or "" when they are equal.
func unifiedDiff(aName, bName string, a, b []byte) string {
	if string(a) == string(b) {
		return ""
	}
	x, y := splitLines(a), splitLines(b)
	ops := diffLines(x, y)

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", aName, bName)
	for start := 0; start < len(ops); {
		// Find the next change and the extent of its hunk, merging changes
		// separated by at most 2*diffContext equal lines.
		for start < len(ops) && ops[start].kind == ' ' {
			start++
		}
		if start == len(ops) {
			break
		}
		lo := max(start-diffContext, 0)
		end := start
		for i := start; i < len(ops); i++ {
			if ops[i].kind != ' ' {
				end = i + 1
			} else if i-end >= 2*diffContext {
				break
			}
		}
		hi := min(end+diffContext, len(ops))

		ai, bi := ops[lo].a, ops[lo].b
		var an, bn int
		for _, op := range ops[lo:hi] {
			if op.kind != '+' {
				an++
			}
			if op.kind != '-' {
				bn++
			}
		}
		fmt.Fprintf(&sb, "@@ -%s +%s @@\n", hunkRange(ai, an), hunkRange(bi, bn))
		for _, op := range ops[lo:hi] {
			line := op.text
			sb.WriteByte(op.kind)
			sb.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				sb.WriteString("\n\\ No newline at end of file\n")
			}
		}
		start = hi
	}
	return sb.String()
}

func hunkRange(start, n int) string {
	if n == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	if n == 1 {
		return fmt.Sprintf("%d", start+1)
	}
	return fmt.Sprintf("%d,%d", start+1, n)
}

type diffOp struct {
	kind byte // ' ', '-' or '+'
	text string
	a, b int // line indexes in a and b before this op
}

func diffLines(x, y []string) []diffOp {
	n, m := len(x), len(y)
	if n*m > maxDiffCells {
		ops := make([]diffOp, 0, n+m)
		for i, line := range x {
			ops = append(ops, diffOp{'-', line, i, 0})
		}
		for j, line := range y {
			ops = append(ops, diffOp{'+', line, n, j})
		}
		return ops
	}
	// lcs[i][j] is the LCS length of x[i:] and y[j:].
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	ops := make([]diffOp, 0, n+m)
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && x[i] == y[j]:
			ops = append(ops, diffOp{' ', x[i], i, j})
			i++
			j++
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, diffOp{'-', x[i], i, j})
			i++
		default:
			ops = append(ops, diffOp{'+', y[j], i, j})
			j++
		}
	}
	return ops
}

func splitLines(b []byte) []string {
	lines := strings.SplitAfter(string(b), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
