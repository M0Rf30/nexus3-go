package cleanup

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/M0Rf30/nexus3-go/pkg/nexus3/cleanup/version"
)

// Accepted values of Policy.ReleaseType.
const (
	// ReleaseAny lets a policy touch every component (the default).
	ReleaseAny = "any"
	// ReleaseOnly restricts a policy to final releases.
	ReleaseOnly = "release"
	// ReleasePrerelease restricts a policy to snapshots and other
	// pre-releases.
	ReleasePrerelease = "prerelease"
)

// Accepted values of Policy.AgeFrom.
const (
	// AgeFromVersionTimestamp derives a component's age from the build
	// timestamp embedded in its version, falling back to the newest asset
	// BlobCreated, then LastModified (the default).
	AgeFromVersionTimestamp = "versionTimestamp"
	// AgeFromUploaded derives a component's age from the newest asset
	// BlobCreated (falling back to LastModified) and ignores the version.
	AgeFromUploaded = "uploaded"
)

// VersionOrderAuto selects the version ordering from the component format
// (see version.SchemeForFormat). It is the default for Policy.VersionOrder.
const VersionOrderAuto = "auto"

// Config is the top-level cleanup configuration file.
type Config struct {
	// Policies are evaluated in order; a component selected by several
	// policies is planned once, under the first policy that selects it.
	Policies []Policy `yaml:"policies"`
}

// Policy describes one retention rule applied to one or more repositories.
// Components are grouped per repository, group and name (so every apt
// architecture, yum architecture or maven groupId:artifactId keeps its own
// history), ordered newest-first and then judged individually.
type Policy struct {
	// Name identifies the policy in reports and error messages. Required and
	// unique within the file.
	Name string `yaml:"name"`
	// Repositories lists the repositories to clean (at least one).
	Repositories []string `yaml:"repositories"`
	// KeepLatest keeps the N newest versions of every group; 0 disables the
	// criterion.
	KeepLatest int `yaml:"keepLatest"`
	// KeepDays, when non-nil, additionally keeps every component whose age
	// is at most this many days, even outside the top KeepLatest (a union,
	// like custom_rule.py of zextras' JFrog cleanup). 0 means "today only";
	// nil disables it.
	KeepDays *int `yaml:"keepDays"`
	// OlderThan deletes components older than this many days; written
	// "90d" or "90" in YAML. 0 disables the criterion.
	OlderThan Days `yaml:"olderThan"`
	// ReleaseType restricts which components the policy may touch: "any"
	// (default), "release" or "prerelease". Components excluded this way are
	// invisible to the policy and do not occupy KeepLatest slots.
	ReleaseType string `yaml:"releaseType"`
	// AgeFrom selects the age source: "versionTimestamp" (default) or
	// "uploaded".
	AgeFrom string `yaml:"ageFrom"`
	// VersionOrder selects how versions are ordered: "auto" (default, from
	// the component format) or a version scheme name (debian, rpm, maven,
	// semver, lexical).
	VersionOrder string `yaml:"versionOrder"`
	// IncludeNames, when non-empty, restricts the policy to components whose
	// name matches at least one of these regular expressions (unanchored;
	// use ^...$ for an exact match).
	IncludeNames []string `yaml:"includeNames"`
	// ExcludeNames removes components whose name matches any of these
	// regular expressions from the policy's scope.
	ExcludeNames []string `yaml:"excludeNames"`
	// ProtectVersions lists regular expressions matched against the version;
	// a matching component is never deleted. Protected components still
	// occupy a KeepLatest slot.
	ProtectVersions []string `yaml:"protectVersions"`
}

// Days is a number of days that unmarshals from YAML as either an integer
// ("90") or an integer with a "d" suffix ("90d").
type Days int

var daysRE = regexp.MustCompile(`(?i)^\s*(\d+)\s*d?\s*$`)

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Days) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: invalid days value: expected a number such as 90 or 90d", node.Line)
	}
	n, err := parseDays(node.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", node.Line, err)
	}
	*d = n
	return nil
}

// String formats d the way it is written in configuration, e.g. "90d".
func (d Days) String() string { return strconv.Itoa(int(d)) + "d" }

func parseDays(s string) (Days, error) {
	m := daysRE.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid days value %q: expected a non-negative number such as 90 or 90d", s)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, fmt.Errorf("invalid days value %q: %w", s, err)
	}
	return Days(n), nil
}

// LoadConfig reads and validates the YAML file at path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cleanup: read config: %w", err)
	}
	cfg, err := ParseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("%w (in %s)", err, path)
	}
	return cfg, nil
}

// ParseConfig decodes and validates YAML configuration. Decoding is strict:
// unknown keys and multiple documents are errors.
func ParseConfig(data []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("cleanup: parse config: empty document")
		}
		return nil, fmt.Errorf("cleanup: parse config: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("cleanup: parse config: %w", err)
		}
		return nil, errors.New("cleanup: parse config: multiple YAML documents are not supported")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks the configuration: at least one policy; each policy named
// (uniquely), with at least one repository and at least one of keepLatest or
// olderThan, valid enums, non-negative numbers and compilable regexes. Errors
// name the offending policy by index and name.
func (c *Config) Validate() error {
	if c == nil || len(c.Policies) == 0 {
		return errors.New("cleanup: config: no policies defined")
	}
	seen := make(map[string]int, len(c.Policies))
	for i := range c.Policies {
		if _, err := compilePolicy(i, &c.Policies[i]); err != nil {
			return err
		}
		if j, dup := seen[c.Policies[i].Name]; dup {
			return policyErr(i, c.Policies[i].Name, "duplicate policy name (also policy[%d])", j)
		}
		seen[c.Policies[i].Name] = i
	}
	return nil
}

// compiledPolicy is a validated Policy with regexes compiled and defaults
// resolved.
type compiledPolicy struct {
	Policy
	include, exclude, protect []*regexp.Regexp
	// scheme is the explicit version scheme; meaningful only when !auto.
	scheme version.Scheme
	auto   bool
	fromTS bool // ageFrom == versionTimestamp
	// releaseOnly / prereleaseOnly restrict by release type.
	releaseOnly, prereleaseOnly bool
}

func policyErr(i int, name, format string, args ...any) error {
	return fmt.Errorf("cleanup: policy[%d] %q: %s", i, name, fmt.Sprintf(format, args...))
}

func compilePolicy(i int, p *Policy) (*compiledPolicy, error) {
	cp := &compiledPolicy{Policy: *p}
	fail := func(format string, args ...any) (*compiledPolicy, error) {
		return nil, policyErr(i, p.Name, format, args...)
	}

	if strings.TrimSpace(p.Name) == "" {
		return fail("name is required")
	}
	if len(p.Repositories) == 0 {
		return fail("at least one repository is required")
	}
	for _, r := range p.Repositories {
		if strings.TrimSpace(r) == "" {
			return fail("repositories contains an empty name")
		}
	}
	if p.KeepLatest < 0 {
		return fail("keepLatest must be >= 0, got %d", p.KeepLatest)
	}
	if p.KeepDays != nil && *p.KeepDays < 0 {
		return fail("keepDays must be >= 0, got %d", *p.KeepDays)
	}
	if p.OlderThan < 0 {
		return fail("olderThan must be >= 0, got %d", int(p.OlderThan))
	}
	if p.KeepLatest == 0 && p.OlderThan == 0 {
		return fail("at least one of keepLatest or olderThan must be set (> 0)")
	}

	switch p.ReleaseType {
	case "", ReleaseAny:
	case ReleaseOnly:
		cp.releaseOnly = true
	case ReleasePrerelease:
		cp.prereleaseOnly = true
	default:
		return fail("invalid releaseType %q (want any, release or prerelease)", p.ReleaseType)
	}

	switch p.AgeFrom {
	case "", AgeFromVersionTimestamp:
		cp.fromTS = true
	case AgeFromUploaded:
	default:
		return fail("invalid ageFrom %q (want versionTimestamp or uploaded)", p.AgeFrom)
	}

	if p.VersionOrder == "" || p.VersionOrder == VersionOrderAuto {
		cp.auto = true
	} else {
		s, err := version.ParseScheme(p.VersionOrder)
		if err != nil {
			return fail("invalid versionOrder %q: %v", p.VersionOrder, err)
		}
		cp.scheme = s
	}

	var err error
	if cp.include, err = compileAll("includeNames", p.IncludeNames); err != nil {
		return fail("%v", err)
	}
	if cp.exclude, err = compileAll("excludeNames", p.ExcludeNames); err != nil {
		return fail("%v", err)
	}
	if cp.protect, err = compileAll("protectVersions", p.ProtectVersions); err != nil {
		return fail("%v", err)
	}
	return cp, nil
}

func compileAll(field string, patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, pat := range patterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid regex %q: %w", field, pat, err)
		}
		out = append(out, re)
	}
	return out, nil
}

func matchAny(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
