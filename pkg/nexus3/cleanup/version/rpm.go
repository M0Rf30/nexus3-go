package version

import "strings"

func compareRPM(a, b string) int {
	ea, ra := splitEpoch(strings.TrimSpace(a))
	eb, rb := splitEpoch(strings.TrimSpace(b))

	if c := compareDigitStrings(ea, eb); c != 0 {
		return c
	}

	va, relA := splitLast(ra, '-')
	vb, relB := splitLast(rb, '-')

	if c := rpmvercmp(va, vb); c != 0 {
		return c
	}

	return rpmvercmp(relA, relB)
}

func isAlnum(c byte) bool { return isDigit(c) || isAlpha(c) }

func at(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}

	return 0
}

// rpmvercmp is a port of rpm's rpmvercmp including '~' and '^' handling.
func rpmvercmp(a, b string) int {
	if a == b {
		return 0
	}

	i, j := 0, 0

	for i < len(a) || j < len(b) {
		for i < len(a) && !isAlnum(a[i]) && a[i] != '~' && a[i] != '^' {
			i++
		}

		for j < len(b) && !isAlnum(b[j]) && b[j] != '~' && b[j] != '^' {
			j++
		}

		ca, cb := at(a, i), at(b, j)

		if ca == '~' || cb == '~' {
			if ca != '~' {
				return 1
			}

			if cb != '~' {
				return -1
			}

			i++
			j++

			continue
		}

		if ca == '^' || cb == '^' {
			switch {
			case ca == 0:
				return -1
			case cb == 0:
				return 1
			case ca != '^':
				return 1
			case cb != '^':
				return -1
			}

			i++
			j++

			continue
		}

		if ca == 0 || cb == 0 {
			break
		}

		si, sj := i, j
		numeric := isDigit(ca)

		if numeric {
			for i < len(a) && isDigit(a[i]) {
				i++
			}

			for j < len(b) && isDigit(b[j]) {
				j++
			}
		} else {
			for i < len(a) && isAlpha(a[i]) {
				i++
			}

			for j < len(b) && isAlpha(b[j]) {
				j++
			}
		}

		sa, sb := a[si:i], b[sj:j]

		if sb == "" {
			if numeric {
				return 1
			}

			return -1
		}

		if numeric {
			if c := compareDigitStrings(sa, sb); c != 0 {
				return c
			}
		} else if c := strings.Compare(sa, sb); c != 0 {
			return c
		}
	}

	switch {
	case i >= len(a) && j >= len(b):
		return 0
	case i < len(a):
		return 1
	}

	return -1
}
