package version

import "strings"

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func allDigits(s string) bool {
	if s == "" {
		return false
	}

	for i := range len(s) {
		if !isDigit(s[i]) {
			return false
		}
	}

	return true
}

// compareDigitStrings compares two digit strings numerically without overflow.
func compareDigitStrings(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")

	if len(a) != len(b) {
		return len(a) - len(b)
	}

	return strings.Compare(a, b)
}

func splitEpoch(v string) (epoch, rest string) {
	if i := strings.IndexByte(v, ':'); i > 0 && allDigits(v[:i]) {
		return v[:i], v[i+1:]
	}

	return "0", v
}

func compareDebian(a, b string) int {
	ea, ra := splitEpoch(strings.TrimSpace(a))
	eb, rb := splitEpoch(strings.TrimSpace(b))

	if c := compareDigitStrings(ea, eb); c != 0 {
		return c
	}

	ua, rva := splitLast(ra, '-')
	ub, rvb := splitLast(rb, '-')

	if c := debVerRev(ua, ub); c != 0 {
		return c
	}

	return debVerRev(rva, rvb)
}

func splitLast(s string, sep byte) (head, tail string) {
	if i := strings.LastIndexByte(s, sep); i >= 0 {
		return s[:i], s[i+1:]
	}

	return s, ""
}

func debOrder(s string, i int) int {
	if i >= len(s) {
		return 0
	}

	c := s[i]

	switch {
	case isDigit(c):
		return 0
	case isAlpha(c):
		return int(c)
	case c == '~':
		return -1
	}

	return int(c) + 256
}

// debVerRev is dpkg's verrevcmp.
func debVerRev(a, b string) int {
	i, j := 0, 0

	for i < len(a) || j < len(b) {
		for (i < len(a) && !isDigit(a[i])) || (j < len(b) && !isDigit(b[j])) {
			ac, bc := debOrder(a, i), debOrder(b, j)
			if ac != bc {
				return ac - bc
			}

			i++
			j++
		}

		si := i
		for i < len(a) && isDigit(a[i]) {
			i++
		}

		sj := j
		for j < len(b) && isDigit(b[j]) {
			j++
		}

		if c := compareDigitStrings(a[si:i], b[sj:j]); c != 0 {
			return c
		}
	}

	return 0
}
