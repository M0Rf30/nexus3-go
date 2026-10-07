package version

import "strings"

type semver struct {
	core []string // numeric components
	pre  []string // pre-release identifiers, empty for releases
}

func parseSemver(v string) (semver, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	v, _, _ = strings.Cut(v, "+")

	core, pre, hasPre := strings.Cut(v, "-")

	var s semver

	for p := range strings.SplitSeq(core, ".") {
		if !allDigits(p) {
			return semver{}, false
		}

		s.core = append(s.core, strings.TrimLeft(p, "0"))
	}

	if hasPre {
		if pre == "" {
			return semver{}, false
		}

		for p := range strings.SplitSeq(pre, ".") {
			if p == "" {
				return semver{}, false
			}

			s.pre = append(s.pre, p)
		}
	}

	return s, true
}

// compareSemver orders by semver 2.0 precedence. Unparsable versions sort
// before parsable ones and fall back to lexical order among themselves,
// which keeps the order total.
func compareSemver(a, b string) int {
	sa, oka := parseSemver(a)
	sb, okb := parseSemver(b)

	switch {
	case !oka && !okb:
		return compareLexical(a, b)
	case !oka:
		return -1
	case !okb:
		return 1
	}

	for i := 0; i < len(sa.core) || i < len(sb.core); i++ {
		var x, y string
		if i < len(sa.core) {
			x = sa.core[i]
		}

		if i < len(sb.core) {
			y = sb.core[i]
		}

		if c := compareDigitStrings(x, y); c != 0 {
			return c
		}
	}

	switch {
	case len(sa.pre) == 0 && len(sb.pre) == 0:
		return 0
	case len(sa.pre) == 0:
		return 1
	case len(sb.pre) == 0:
		return -1
	}

	for i := 0; i < len(sa.pre) && i < len(sb.pre); i++ {
		x, y := sa.pre[i], sb.pre[i]
		nx, ny := allDigits(x), allDigits(y)

		var c int

		switch {
		case nx && ny:
			c = compareDigitStrings(x, y)
		case nx:
			c = -1
		case ny:
			c = 1
		default:
			c = strings.Compare(x, y)
		}

		if c != 0 {
			return c
		}
	}

	return len(sa.pre) - len(sb.pre)
}
