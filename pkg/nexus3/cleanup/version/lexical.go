package version

import "strings"

// lexTokens splits s into alternating digit and non-digit runs.
func lexTokens(s string) []string {
	var out []string

	for i := 0; i < len(s); {
		j := i
		d := isDigit(s[i])

		for j < len(s) && isDigit(s[j]) == d {
			j++
		}

		out = append(out, s[i:j])
		i = j
	}

	return out
}

// compareLexical is a natural sort: digit runs compare numerically, other
// runs bytewise; ties are broken by plain string comparison for totality.
func compareLexical(a, b string) int {
	ta, tb := lexTokens(a), lexTokens(b)

	for i := 0; i < len(ta) && i < len(tb); i++ {
		x, y := ta[i], tb[i]
		dx, dy := isDigit(x[0]), isDigit(y[0])

		var c int

		switch {
		case dx && dy:
			c = compareDigitStrings(x, y)
		case dx:
			c = -1
		case dy:
			c = 1
		default:
			c = strings.Compare(x, y)
		}

		if c != 0 {
			return c
		}
	}

	if c := len(ta) - len(tb); c != 0 {
		return c
	}

	return strings.Compare(a, b)
}
