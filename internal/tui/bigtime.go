package tui

import "strings"

// Seven-segment layout per digit: a top, b top-right, c bottom-right,
// d bottom, e bottom-left, f top-left, g middle.
var segments = map[rune][7]bool{
	//    a      b      c      d      e      f      g
	'0': {true, true, true, true, true, true, false},
	'1': {false, true, true, false, false, false, false},
	'2': {true, true, false, true, true, false, true},
	'3': {true, true, true, true, false, false, true},
	'4': {false, true, true, false, false, true, true},
	'5': {true, false, true, true, false, true, true},
	'6': {true, false, true, true, true, true, true},
	'7': {true, true, true, false, false, false, false},
	'8': {true, true, true, true, true, true, true},
	'9': {true, true, true, true, false, true, true},
}

const bigTimeRows = 5

// bigTime renders a digits-and-colons string (e.g. "01:23:45") as a 5-row
// seven-segment block figure. Unknown runes render as a blank digit cell.
func bigTime(s string) []string {
	rows := make([]strings.Builder, bigTimeRows)
	for i, r := range s {
		if i > 0 {
			for j := range rows {
				rows[j].WriteByte(' ')
			}
		}
		if r == ':' {
			for j := range rows {
				if j == 1 || j == 3 {
					rows[j].WriteRune('█')
				} else {
					rows[j].WriteByte(' ')
				}
			}
			continue
		}
		seg := segments[r]
		a, b, c, d, e, f, g := seg[0], seg[1], seg[2], seg[3], seg[4], seg[5], seg[6]
		writeDigitRow(&rows[0], a, f || a, b || a)
		writeDigitRow(&rows[1], false, f, b)
		writeDigitRow(&rows[2], g, f || e || g, b || c || g)
		writeDigitRow(&rows[3], false, e, c)
		writeDigitRow(&rows[4], d, e || d, c || d)
	}
	out := make([]string, bigTimeRows)
	for i := range rows {
		out[i] = rows[i].String()
	}
	return out
}

// writeDigitRow appends one 4-cell digit row: edge, two middle cells
// (lit when `mid`), edge.
func writeDigitRow(b *strings.Builder, mid, left, right bool) {
	cell := func(on bool) {
		if on {
			b.WriteRune('█')
		} else {
			b.WriteByte(' ')
		}
	}
	cell(left)
	cell(mid)
	cell(mid)
	cell(right)
}

// bigTimeWidth is the rendered cell width of s: digits are 4 wide, colons 1,
// with 1-cell gaps.
func bigTimeWidth(s string) int {
	w := 0
	for i, r := range s {
		if i > 0 {
			w++
		}
		if r == ':' {
			w++
		} else {
			w += 4
		}
	}
	return w
}
