// Package version implements version-string parsing, ordering and
// classification for the package formats served by Nexus (Debian, RPM,
// Maven, Semver and a lexical fallback), in pure Go.
package version

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Scheme identifies a version ordering scheme.
type Scheme string

// Supported version schemes.
const (
	Debian  Scheme = "debian"
	RPM     Scheme = "rpm"
	Maven   Scheme = "maven"
	Semver  Scheme = "semver"
	Lexical Scheme = "lexical"
)

// ParseScheme converts a scheme name into a Scheme.
func ParseScheme(s string) (Scheme, error) {
	switch Scheme(strings.ToLower(strings.TrimSpace(s))) {
	case Debian:
		return Debian, nil
	case RPM:
		return RPM, nil
	case Maven:
		return Maven, nil
	case Semver:
		return Semver, nil
	case Lexical:
		return Lexical, nil
	}

	return "", fmt.Errorf("version: unknown scheme %q (want debian, rpm, maven, semver or lexical)", s)
}

// SchemeForFormat returns the natural version scheme of a Nexus repository format.
func SchemeForFormat(format string) Scheme {
	switch strings.ToLower(format) {
	case "apt":
		return Debian
	case "yum":
		return RPM
	case "maven2":
		return Maven
	case "npm", "helm", "go", "nuget", "pypi", "rubygems", "cargo", "composer":
		return Semver
	}

	return Lexical
}

// Compare orders two versions under scheme s, returning -1, 0 or 1.
// It defines a total preorder and never panics, even on garbage input.
func Compare(s Scheme, a, b string) int {
	var c int

	switch s {
	case Debian:
		c = compareDebian(a, b)
	case RPM:
		c = compareRPM(a, b)
	case Maven:
		c = compareMaven(a, b)
	case Semver:
		c = compareSemver(a, b)
	default:
		c = compareLexical(a, b)
	}

	return sign(c)
}

func sign(c int) int {
	switch {
	case c < 0:
		return -1
	case c > 0:
		return 1
	}

	return 0
}

var preTokens = map[string]bool{
	qAlpha: true, qBeta: true, "rc": true, "pre": true, qSnapshot: true, "devel": true,
}

// IsPrerelease reports whether v looks like a pre-release under scheme s.
func IsPrerelease(s Scheme, v string) bool {
	switch s {
	case Maven:
		return strings.Contains(strings.ToLower(v), qSnapshot) || mavenTimestamp.MatchString(v)
	case Semver:
		core, _, _ := strings.Cut(v, "+")

		return strings.Contains(core, "-")
	default:
		return hasPreToken(v)
	}
}

func hasPreToken(v string) bool {
	if strings.Contains(v, "~") {
		return true
	}

	for tok := range strings.FieldsFuncSeq(strings.ToLower(v), func(r rune) bool { return r < 'a' || r > 'z' }) {
		if preTokens[tok] {
			return true
		}
	}

	return false
}

var (
	buildRun   = regexp.MustCompile(`\d+`)
	buildMaven = regexp.MustCompile(`(?:^|\D)(\d{8})\.(\d{6})(?:\D|$)`)
)

// BuildTime extracts a UTC build timestamp embedded in v: either a 14-digit
// YYYYMMDDhhmmss run or a Maven yyyyMMdd.HHmmss pair. It reports false when
// none is present or the digits do not form a real date.
func BuildTime(v string) (time.Time, bool) {
	for _, run := range buildRun.FindAllString(v, -1) {
		if len(run) != 14 {
			continue
		}

		if t, ok := parseStamp(run[:8], run[8:]); ok {
			return t, true
		}
	}

	for _, m := range buildMaven.FindAllStringSubmatch(v, -1) {
		if t, ok := parseStamp(m[1], m[2]); ok {
			return t, true
		}
	}

	return time.Time{}, false
}

func parseStamp(date, clock string) (time.Time, bool) {
	t, err := time.Parse("20060102150405", date+clock)
	if err != nil || t.Year() < 1990 || t.Year() > 2200 {
		return time.Time{}, false
	}

	return t.UTC(), true
}
