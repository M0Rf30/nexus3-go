package version

import (
	"testing"
	"time"
)

func TestCompareOrdered(t *testing.T) {
	// Each chain must be strictly ascending.
	tests := []struct {
		name   string
		scheme Scheme
		chain  []string
	}{
		{"deb zextras", Debian, []string{"0.10.9-20251104144405ubuntu", "0.10.9-20251104164116ubuntu"}},
		{"deb devel", Debian, []string{"0.13.0-devel.28-20260513071311ubuntu", "0.13.0-devel.29-20260513071311ubuntu", "0.13.0-devel.100-20260101000000ubuntu"}},
		{"deb r", Debian, []string{"0.164.r3108-20260408155731jammy", "0.165.r3222-1"}},
		{"deb tilde", Debian, []string{"1.0~~", "1.0~rc1", "1.0", "1.0a", "1.0+b1", "1.1"}},
		{"deb epoch", Debian, []string{"2.0", "1:0.1", "2:0.0"}},
		{"deb revision", Debian, []string{"1.0-1", "1.0-1+b1", "1.0-2", "1.0-10"}},
		{"deb numeric", Debian, []string{"1.2", "1.10", "1.010a"}},
		{"rpm zextras", RPM, []string{"4.27.15-20260602053857.el9", "4.27.15-20260603053857.el9", "4.27.16-1.el9"}},
		{"rpm tilde", RPM, []string{"1.0~rc1", "1.0", "1.0^git1", "1.0.1"}},
		{"rpm caret", RPM, []string{"1.0^", "1.0^git1", "1.0^git2", "1.0.a"}},
		{"rpm basic", RPM, []string{"1.0", "1.0a", "1.1", "1.10", "2"}},
		{"rpm epoch", RPM, []string{"9.0", "1:0.1"}},
		{"rpm num vs alpha", RPM, []string{"1.a", "1.1"}},
		{"maven qualifiers", Maven, []string{"1.0-alpha", "1.0-beta", "1.0-milestone", "1.0-rc", "1.0-SNAPSHOT", "1.0", "1.0-sp", "1.0.1"}},
		{"maven numeric", Maven, []string{"1.2", "1.10", "2"}},
		{"maven snapshot ts", Maven, []string{
			"1.3.9", "1.4.0-20260513.080833-1", "1.4.0-20260513.080833-2", "1.4.0-20260514.000000-1", "1.4.0",
		}},
		{"maven a1", Maven, []string{"1.0-a1", "1.0-b1", "1.0-m1", "1.0-rc1"}},
		{"semver", Semver, []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1", "v2"}},
		{"semver unparsable first", Semver, []string{"abc", "0.0.1"}},
		{"lexical", Lexical, []string{"a2", "a10", "b1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := range len(tt.chain) {
				for j := i + 1; j < len(tt.chain); j++ {
					a, b := tt.chain[i], tt.chain[j]
					if got := Compare(tt.scheme, a, b); got != -1 {
						t.Errorf("Compare(%q,%q) = %d, want -1", a, b, got)
					}

					if got := Compare(tt.scheme, b, a); got != 1 {
						t.Errorf("Compare(%q,%q) = %d, want 1", b, a, got)
					}
				}
			}
		})
	}
}

func TestCompareEqual(t *testing.T) {
	tests := []struct {
		scheme Scheme
		a, b   string
	}{
		{Debian, "1.0", "0:1.0"},
		{Debian, "1.0-0", "1.0"},
		{RPM, "1.0", "0:1.0"},
		{RPM, "1_0", "1.0"},
		{Maven, "1", "1.0.0"},
		{Maven, "1.0-ga", "1.0"},
		{Maven, "1.0-final", "1.0-release"},
		{Maven, "1.0-cr1", "1.0-rc1"},
		{Semver, "v1.2.3", "1.2.3+build.5"},
		{Semver, "1.2", "1.2.0"},
		{Lexical, "abc", "abc"},
	}

	for _, tt := range tests {
		if got := Compare(tt.scheme, tt.a, tt.b); got != 0 {
			t.Errorf("%s: Compare(%q,%q) = %d, want 0", tt.scheme, tt.a, tt.b, got)
		}
	}
}

func TestCompareAntisymmetryAndTransitivity(t *testing.T) {
	samples := []string{
		"", "0", "1", "1.0", "1.0.0", "1.0~rc1", "1.0^git1", "1:0.1", "2.0", "v1.2.3", "1.2.3-alpha", "1.2.3-alpha.1",
		"1.0-SNAPSHOT", "1.0-sp", "1.4.0-20260513.080833-1", "0.10.9-20251104144405ubuntu", "0.13.0-devel.28-20260513071311ubuntu",
		"0.164.r3108-20260408155731jammy", "4.27.15-20260602053857.el9", "abc", "a10", "a2", "1.0-", "--", "..", ":", "~", "^",
		"99999999999999999999999999", "1.0.0.0.0.1", "é", "1:", ":1", "-1", "1-1-1",
	}

	for _, s := range []Scheme{Debian, RPM, Maven, Semver, Lexical, Scheme("bogus")} {
		for _, a := range samples {
			if Compare(s, a, a) != 0 {
				t.Errorf("%s: Compare(%q,%q) != 0", s, a, a)
			}

			for _, b := range samples {
				ab, ba := Compare(s, a, b), Compare(s, b, a)
				if ab != -ba {
					t.Errorf("%s: antisymmetry violated for %q,%q: %d vs %d", s, a, b, ab, ba)
				}

				for _, c := range samples {
					if ab <= 0 && Compare(s, b, c) <= 0 && Compare(s, a, c) > 0 {
						t.Errorf("%s: transitivity violated for %q <= %q <= %q", s, a, b, c)
					}
				}
			}
		}
	}
}

func TestParseScheme(t *testing.T) {
	for _, n := range []string{"debian", "rpm", "maven", "semver", "lexical", " RPM "} {
		if _, err := ParseScheme(n); err != nil {
			t.Errorf("ParseScheme(%q): %v", n, err)
		}
	}

	if _, err := ParseScheme("nope"); err == nil {
		t.Error("expected error")
	}
}

func TestSchemeForFormat(t *testing.T) {
	tests := map[string]Scheme{
		"apt": Debian, "yum": RPM, "maven2": Maven, "npm": Semver, "helm": Semver, "go": Semver,
		"nuget": Semver, "pypi": Semver, "rubygems": Semver, "cargo": Semver, "composer": Semver,
		"raw": Lexical, "docker": Lexical, "": Lexical,
	}

	for f, want := range tests {
		if got := SchemeForFormat(f); got != want {
			t.Errorf("SchemeForFormat(%q) = %s, want %s", f, got, want)
		}
	}
}

func TestIsPrerelease(t *testing.T) {
	tests := []struct {
		scheme Scheme
		v      string
		want   bool
	}{
		{Maven, "1.0-SNAPSHOT", true},
		{Maven, "1.4.0-20260513.080833-1", true},
		{Maven, "1.0", false},
		{Maven, "1.0-rc1", false},
		{Semver, "1.0.0-rc.1", true},
		{Semver, "1.0.0", false},
		{Semver, "1.0.0+build-5", false},
		{Debian, "1.0~rc1", true},
		{Debian, "0.13.0-devel.28-20260513071311ubuntu", true},
		{Debian, "0.10.9-20251104144405ubuntu", false},
		{Debian, "1.0-RC1", true},
		{Debian, "1.0.beta2", true},
		{Debian, "0.164.r3108-20260408155731jammy", false},
		{RPM, "4.27.15-20260602053857.el9", false},
		{RPM, "1.0-0.pre1.el9", true},
		{RPM, "1.0-1.precious", false},
		{Lexical, "1.0-alpha", true},
	}

	for _, tt := range tests {
		if got := IsPrerelease(tt.scheme, tt.v); got != tt.want {
			t.Errorf("IsPrerelease(%s,%q) = %v, want %v", tt.scheme, tt.v, got, tt.want)
		}
	}
}

func TestBuildTime(t *testing.T) {
	tests := []struct {
		v    string
		want time.Time
		ok   bool
	}{
		{"0.10.9-20251104144405ubuntu", time.Date(2025, 11, 4, 14, 44, 5, 0, time.UTC), true},
		{"4.27.15-20260602053857.el9", time.Date(2026, 6, 2, 5, 38, 57, 0, time.UTC), true},
		{"0.13.0-devel.28-20260513071311ubuntu", time.Date(2026, 5, 13, 7, 13, 11, 0, time.UTC), true},
		{"1.4.0-20260513.080833-1", time.Date(2026, 5, 13, 8, 8, 33, 0, time.UTC), true},
		{"99999999999999", time.Time{}, false},
		{"20261340000000", time.Time{}, false},
		{"20260230000000", time.Time{}, false},
		{"20260101250000", time.Time{}, false},
		{"2026060205385712", time.Time{}, false},
		{"1.2.3", time.Time{}, false},
		{"", time.Time{}, false},
		{"1.0-20261301.080833-1", time.Time{}, false},
	}

	for _, tt := range tests {
		got, ok := BuildTime(tt.v)
		if ok != tt.ok || !got.Equal(tt.want) {
			t.Errorf("BuildTime(%q) = %v,%v want %v,%v", tt.v, got, ok, tt.want, tt.ok)
		}
	}
}
