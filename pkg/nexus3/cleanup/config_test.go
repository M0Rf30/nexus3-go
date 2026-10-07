package cleanup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfig_Valid(t *testing.T) {
	cfg, err := ParseConfig([]byte(`
policies:
  - name: ubuntu-rc
    repositories: [ubuntu-rc, ubuntu-rc2]
    keepLatest: 3
    keepDays: 14
    releaseType: release
    ageFrom: uploaded
    versionOrder: debian
    includeNames: ["^zextras-"]
    excludeNames: ["-dbg$"]
    protectVersions: ["^1\\.0\\."]
  - name: snapshots
    repositories: [maven-snapshots]
    olderThan: 90d
  - name: plain-number
    repositories: [x]
    olderThan: 30
    keepDays: 0
  - name: zero
    repositories: [x]
    keepLatest: 1
    olderThan: 0
`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if len(cfg.Policies) != 4 {
		t.Fatalf("policies = %d", len(cfg.Policies))
	}
	p := cfg.Policies[0]
	if p.Name != "ubuntu-rc" || len(p.Repositories) != 2 || p.KeepLatest != 3 ||
		p.KeepDays == nil || *p.KeepDays != 14 || p.ReleaseType != "release" ||
		p.AgeFrom != "uploaded" || p.VersionOrder != "debian" ||
		len(p.IncludeNames) != 1 || len(p.ExcludeNames) != 1 || len(p.ProtectVersions) != 1 {
		t.Errorf("policy[0] = %+v", p)
	}
	if cfg.Policies[1].OlderThan != 90 || cfg.Policies[1].KeepDays != nil {
		t.Errorf("policy[1] = %+v", cfg.Policies[1])
	}
	if cfg.Policies[2].OlderThan != 30 || cfg.Policies[2].KeepDays == nil || *cfg.Policies[2].KeepDays != 0 {
		t.Errorf("policy[2] = %+v", cfg.Policies[2])
	}
}

func TestParseConfig_Errors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []string // substrings that must all appear
	}{
		{"empty", "", []string{"empty document"}},
		{"no policies", "policies: []\n", []string{"no policies"}},
		{"unknown top-level key", "polcies: []\n", []string{"polcies"}},
		{"unknown policy key", "policies:\n  - name: a\n    repositories: [r]\n    keepLatest: 1\n    keepLastest: 2\n", []string{"keepLastest"}},
		{"multiple documents", "policies:\n  - name: a\n    repositories: [r]\n    keepLatest: 1\n---\npolicies: []\n", []string{"multiple"}},
		{"missing name", "policies:\n  - repositories: [r]\n    keepLatest: 1\n", []string{"policy[0]", "name is required"}},
		{"missing repositories", "policies:\n  - name: a\n    keepLatest: 1\n", []string{`policy[0] "a"`, "repository"}},
		{"empty repository", "policies:\n  - name: a\n    repositories: [\"\"]\n    keepLatest: 1\n", []string{`policy[0] "a"`, "empty name"}},
		{"missing criteria", "policies:\n  - name: ok\n    repositories: [r]\n    keepLatest: 1\n  - name: b\n    repositories: [r]\n", []string{`policy[1] "b"`, "keepLatest or olderThan"}},
		{"keepDays alone is no criterion", "policies:\n  - name: b\n    repositories: [r]\n    keepDays: 5\n", []string{`policy[0] "b"`, "keepLatest or olderThan"}},
		{"negative keepLatest", "policies:\n  - name: a\n    repositories: [r]\n    keepLatest: -1\n    olderThan: 5\n", []string{"keepLatest must be >= 0"}},
		{"negative keepDays", "policies:\n  - name: a\n    repositories: [r]\n    keepLatest: 1\n    keepDays: -1\n", []string{"keepDays must be >= 0"}},
		{"bad days word", "policies:\n  - name: a\n    repositories: [r]\n    olderThan: soon\n", []string{"invalid days value", "soon"}},
		{"bad days negative", "policies:\n  - name: a\n    repositories: [r]\n    olderThan: -5\n", []string{"invalid days value"}},
		{"bad days fraction", "policies:\n  - name: a\n    repositories: [r]\n    olderThan: 1.5d\n", []string{"invalid days value"}},
		{"bad days unit", "policies:\n  - name: a\n    repositories: [r]\n    olderThan: 3w\n", []string{"invalid days value"}},
		{"bad days list", "policies:\n  - name: a\n    repositories: [r]\n    olderThan: [1]\n", []string{"invalid days value"}},
		{"bad includeNames regex", "policies:\n  - name: a\n    repositories: [r]\n    keepLatest: 1\n    includeNames: [\"(\"]\n", []string{`policy[0] "a"`, "includeNames", "invalid regex"}},
		{"bad excludeNames regex", "policies:\n  - name: a\n    repositories: [r]\n    keepLatest: 1\n    excludeNames: [\"[\"]\n", []string{"excludeNames", "invalid regex"}},
		{"bad protectVersions regex", "policies:\n  - name: a\n    repositories: [r]\n    keepLatest: 1\n    protectVersions: [\"*\"]\n", []string{"protectVersions", "invalid regex"}},
		{"bad releaseType", "policies:\n  - name: a\n    repositories: [r]\n    keepLatest: 1\n    releaseType: stable\n", []string{"releaseType", "stable"}},
		{"bad ageFrom", "policies:\n  - name: a\n    repositories: [r]\n    keepLatest: 1\n    ageFrom: created\n", []string{"ageFrom", "created"}},
		{"bad versionOrder", "policies:\n  - name: a\n    repositories: [r]\n    keepLatest: 1\n    versionOrder: calver\n", []string{"versionOrder", "calver"}},
		{"duplicate name", "policies:\n  - name: a\n    repositories: [r]\n    keepLatest: 1\n  - name: a\n    repositories: [r]\n    keepLatest: 1\n", []string{`policy[1] "a"`, "duplicate"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(tt.yaml))
			if err == nil {
				t.Fatalf("expected error, got config %+v", cfg)
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
			if !strings.HasPrefix(err.Error(), "cleanup: ") {
				t.Errorf("error %q lacks the cleanup: prefix", err)
			}
		})
	}
}

func TestParseDays(t *testing.T) {
	tests := []struct {
		in      string
		want    Days
		wantErr bool
	}{
		{"90", 90, false},
		{"90d", 90, false},
		{"90D", 90, false},
		{" 7 d ", 7, false},
		{"0", 0, false},
		{"", 0, true},
		{"d", 0, true},
		{"-1", 0, true},
		{"1.5", 0, true},
		{"9d9", 0, true},
		{"99999999999999999999", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseDays(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
	if s := Days(90).String(); s != "90d" {
		t.Errorf("String() = %q", s)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yaml")
	if err := os.WriteFile(good, []byte("policies:\n  - name: a\n    repositories: [r]\n    keepLatest: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(good)
	if err != nil || len(cfg.Policies) != 1 {
		t.Fatalf("LoadConfig = %+v, %v", cfg, err)
	}

	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("policies:\n  - name: a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(bad); err == nil || !strings.Contains(err.Error(), "bad.yaml") {
		t.Errorf("LoadConfig(bad) = %v, want error naming the file", err)
	}

	if _, err := LoadConfig(filepath.Join(dir, "missing.yaml")); err == nil || !strings.HasPrefix(err.Error(), "cleanup: read config: ") {
		t.Errorf("LoadConfig(missing) = %v", err)
	}
}
