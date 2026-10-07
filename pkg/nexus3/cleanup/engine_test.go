package cleanup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v3 "github.com/sonatype-nexus-community/nexus-repo-api-client-go/v3"
)

// testNow mirrors TODAY of zextras' test_custom_rule.py, at midday so that
// calendar-day arithmetic is exercised. With keepDays 14 the inclusive cutoff
// date is 2026-09-07.
var testNow = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// fakeAPI is an in-memory API.
type fakeAPI struct {
	mu       sync.Mutex
	repos    map[string][]v3.ComponentXO
	listErr  map[string]error
	listed   map[string]int
	deleted  []string
	failIDs  map[string]error
	onDelete func(ctx context.Context, id string) error
}

func newFake() *fakeAPI {
	return &fakeAPI{
		repos:   map[string][]v3.ComponentXO{},
		listErr: map[string]error{},
		listed:  map[string]int{},
		failIDs: map[string]error{},
	}
}

func (f *fakeAPI) add(repo string, comps ...v3.ComponentXO) {
	f.repos[repo] = append(f.repos[repo], comps...)
}

func (f *fakeAPI) AllComponents(_ context.Context, repo string) iter.Seq2[v3.ComponentXO, error] {
	f.mu.Lock()
	f.listed[repo]++
	f.mu.Unlock()
	return func(yield func(v3.ComponentXO, error) bool) {
		for _, c := range f.repos[repo] {
			if !yield(c, nil) {
				return
			}
		}
		if err := f.listErr[repo]; err != nil {
			yield(v3.ComponentXO{}, err)
		}
	}
}

func (f *fakeAPI) DeleteComponent(ctx context.Context, id string) error {
	if f.onDelete != nil {
		if err := f.onDelete(ctx, id); err != nil {
			return err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failIDs[id]; err != nil {
		return err
	}
	f.deleted = append(f.deleted, id)
	return nil
}

// comp builds a component whose single asset was uploaded on created.
func comp(repo, format, group, name, ver string, created time.Time) v3.ComponentXO {
	id := strings.Join([]string{repo, group, name, ver}, "|")
	asset := v3.AssetXO{FileSize: new(int64(1000)), BlobCreated: &created}
	return v3.ComponentXO{
		Id: &id, Repository: &repo, Format: &format, Group: &group, Name: &name, Version: &ver,
		Assets: []v3.AssetXO{asset},
	}
}

// debBuilds mirrors builds() of the zextras tests: pkg_1.0.<i>-1jammy for
// arch, uploaded on the given dates.
func debBuilds(arch string, dates ...string) []v3.ComponentXO {
	out := make([]v3.ComponentXO, len(dates))
	for i, d := range dates {
		out[i] = comp("ubuntu-rc", "apt", arch, "pkg", debVer(i), day(d))
	}
	return out
}

func debVer(i int) string { return fmt.Sprintf("1.0.%d-1jammy", i) }

func plan(t *testing.T, api API, p ...Policy) *Plan {
	t.Helper()
	pl, err := NewEngine(api, Options{Now: func() time.Time { return testNow }}).Plan(context.Background(), &Config{Policies: p})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return pl
}

// deletedVersions returns "group/version" of every planned item, sorted — the
// analogue of deleted_by() in the zextras tests.
func deletedVersions(pl *Plan) []string {
	out := make([]string, len(pl.Items))
	for i := range pl.Items {
		out[i] = pl.Items[i].Group + "/" + pl.Items[i].Version
	}
	slices.Sort(out)
	return out
}

func amd(i int) string { return "amd64/" + debVer(i) }

func assertDeleted(t *testing.T, pl *Plan, want ...string) {
	t.Helper()
	got := deletedVersions(pl)
	if want == nil {
		want = []string{}
	}
	if !slices.Equal(got, want) {
		t.Errorf("deleted = %v, want %v", got, want)
	}
}

// --- ports of tests/test_custom_rule.py -----------------------------------

func TestZextras_ActivePackageKeepsWholeWindow(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64", "2026-09-08", "2026-09-11", "2026-09-14", "2026-09-17", "2026-09-20")...)
	pl := plan(t, api, Policy{Name: "rc", Repositories: []string{"ubuntu-rc"}, KeepLatest: 3, KeepDays: new(14)})
	assertDeleted(t, pl)
}

func TestZextras_StaleVersionsOutsideWindowAreDeleted(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64",
		"2026-01-01", "2026-02-01", "2026-03-01", "2026-04-01",
		"2026-09-09", "2026-09-13", "2026-09-19")...)
	pl := plan(t, api, Policy{Name: "rc", Repositories: []string{"ubuntu-rc"}, KeepLatest: 3, KeepDays: new(14)})
	assertDeleted(t, pl, amd(0), amd(1), amd(2), amd(3))
}

func TestZextras_DormantPackageKeepsCountAsFloor(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64",
		"2025-01-01", "2025-02-01", "2025-03-01", "2025-04-01", "2025-05-01", "2025-06-01")...)
	pl := plan(t, api, Policy{Name: "rc", Repositories: []string{"ubuntu-rc"}, KeepLatest: 3, KeepDays: new(14)})
	assertDeleted(t, pl, amd(0), amd(1), amd(2))
}

func TestZextras_SingleRecentBuildAddsNoStaleExtras(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64",
		"2025-01-01", "2025-02-01", "2025-03-01", "2025-04-01", "2025-05-01", "2026-09-20")...)
	pl := plan(t, api, Policy{Name: "rc", Repositories: []string{"ubuntu-rc"}, KeepLatest: 3, KeepDays: new(14)})
	assertDeleted(t, pl, amd(0), amd(1), amd(2))
}

func TestZextras_CutoffDateIsInclusive(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64", "2025-01-01", "2025-01-02", "2025-01-03", "2025-01-04")...)
	// Lower versions than every build above, so both fall outside keepLatest 1.
	api.add("ubuntu-rc",
		comp("ubuntu-rc", "apt", "amd64", "pkg", "0.0.1-1jammy", day("2026-09-07")), // exactly on the cutoff
		comp("ubuntu-rc", "apt", "amd64", "pkg", "0.0.2-1jammy", day("2026-09-06")), // one day before
	)
	pl := plan(t, api, Policy{Name: "rc", Repositories: []string{"ubuntu-rc"}, KeepLatest: 1, KeepDays: new(14)})
	got := deletedVersions(pl)
	if slices.Contains(got, "amd64/0.0.1-1jammy") {
		t.Errorf("build on the cutoff date must be kept, deleted = %v", got)
	}
	if !slices.Contains(got, "amd64/0.0.2-1jammy") {
		t.Errorf("build the day before the cutoff must be deleted, deleted = %v", got)
	}
}

func TestZextras_KeepDaysUnsetIsPureKeepN(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64",
		"2026-01-01", "2026-02-01", "2026-03-01", "2026-04-01",
		"2026-09-09", "2026-09-13", "2026-09-19")...)
	pl := plan(t, api, Policy{Name: "rc", Repositories: []string{"ubuntu-rc"}, KeepLatest: 3})
	assertDeleted(t, pl, amd(0), amd(1), amd(2), amd(3))
}

func TestZextras_KeepDaysZeroIsTodayOnlyWindow(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64", "2025-01-01", "2026-09-21", "2026-09-21")...)
	pol := Policy{Name: "rc", Repositories: []string{"ubuntu-rc"}, KeepLatest: 1, KeepDays: new(0)}
	assertDeleted(t, plan(t, api, pol), amd(0))

	pol.KeepDays = nil // same inputs, union disabled
	assertDeleted(t, plan(t, api, pol), amd(0), amd(1))
}

func TestZextras_ArchitecturesAreGroupedIndependently(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64", "2025-01-01", "2025-01-02", "2025-01-03", "2025-01-04")...)
	api.add("ubuntu-rc", debBuilds("arm64", "2025-01-01", "2025-01-02")...)
	pl := plan(t, api, Policy{Name: "rc", Repositories: []string{"ubuntu-rc"}, KeepLatest: 3, KeepDays: new(14)})
	assertDeleted(t, pl, amd(0))
}

// --- other decision semantics ----------------------------------------------

func TestPlan_OlderThanOnly(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64", "2026-01-01", "2026-06-01", "2026-06-23", "2026-06-24", "2026-09-20")...)
	// now = 2026-09-21: 06-23 is 90 days old (kept), 06-01 is 112 days old.
	pl := plan(t, api, Policy{Name: "old", Repositories: []string{"ubuntu-rc"}, OlderThan: 90})
	assertDeleted(t, pl, amd(0), amd(1))
	for _, it := range pl.Items {
		if !strings.HasPrefix(it.Reason, "age ") || !strings.HasSuffix(it.Reason, "d > 90d") {
			t.Errorf("reason = %q", it.Reason)
		}
		if it.Age <= 90*24*time.Hour {
			t.Errorf("age = %v", it.Age)
		}
	}
	// Newest first: the 2026-06-01 build, 112 calendar days before 2026-09-21.
	if pl.Items[0].Reason != "age 112d > 90d" || pl.Items[1].Reason != "age 263d > 90d" {
		t.Errorf("reasons = %q, %q", pl.Items[0].Reason, pl.Items[1].Reason)
	}
}

func TestPlan_OlderThanKeepsEverythingWhenRecent(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64", "2026-09-01", "2026-09-10", "2026-09-20")...)
	assertDeleted(t, plan(t, api, Policy{Name: "old", Repositories: []string{"ubuntu-rc"}, OlderThan: 30}))
}

func TestPlan_BothCriteriaRequireRankAndAge(t *testing.T) {
	api := newFake()
	// v0..v5 oldest to newest; ages: v0 old, v1 old, v2 old, v3 recent, v4 recent, v5 recent.
	api.add("ubuntu-rc", debBuilds("amd64",
		"2025-01-01", "2025-02-01", "2025-03-01", "2026-09-10", "2026-09-15", "2026-09-20")...)
	pol := Policy{Name: "both", Repositories: []string{"ubuntu-rc"}, KeepLatest: 4, OlderThan: 30}
	// Outside top 4: v0, v1 — both old → deleted. v2 is rank 4 → kept (old but inside top N).
	pl := plan(t, api, pol)
	assertDeleted(t, pl, amd(0), amd(1))
	if got := pl.Items[0].Reason; !strings.Contains(got, "rank ") || !strings.Contains(got, "> keepLatest 4") || !strings.Contains(got, "> 30d") {
		t.Errorf("reason = %q", got)
	}

	// Outside top 2 but recent: nothing but old ones go. v3 (rank 3) is recent → kept.
	pol.KeepLatest = 2
	assertDeleted(t, plan(t, api, pol), amd(0), amd(1), amd(2))
}

func TestPlan_BothCriteriaWithKeepDaysUnion(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64", "2025-01-01", "2026-09-01", "2026-09-15", "2026-09-20")...)
	// keepLatest 1, olderThan 5: v1 (20d) and v2 (6d) are outside the top and
	// older than 5d; keepDays 10 rescues v2 only. v0 is long gone.
	pl := plan(t, api, Policy{Name: "u", Repositories: []string{"ubuntu-rc"}, KeepLatest: 1, OlderThan: 5, KeepDays: new(10)})
	assertDeleted(t, pl, amd(0), amd(1))
	if !strings.Contains(pl.Items[0].Reason, "keepDays 10") {
		t.Errorf("reason = %q", pl.Items[0].Reason)
	}
}

func TestPlan_KeepLatestReason(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64", "2025-01-01", "2025-01-02", "2025-01-03")...)
	pl := plan(t, api, Policy{Name: "k", Repositories: []string{"ubuntu-rc"}, KeepLatest: 1})
	if len(pl.Items) != 2 {
		t.Fatalf("items = %+v", pl.Items)
	}
	// Ordered newest-first inside the group: rank 2 then rank 3.
	if pl.Items[0].Reason != "rank 2 > keepLatest 1" || pl.Items[1].Reason != "rank 3 > keepLatest 1" {
		t.Errorf("reasons = %q, %q", pl.Items[0].Reason, pl.Items[1].Reason)
	}
	if pl.Items[0].Version != debVer(1) || pl.Items[0].Policy != "k" || pl.Items[0].Repository != "ubuntu-rc" {
		t.Errorf("item = %+v", pl.Items[0])
	}
}

func TestPlan_VersionOrderingIsNotLexical(t *testing.T) {
	api := newFake()
	// Lexically 1.0.9 > 1.0.10; by Debian order 1.0.10 is newest.
	api.add("ubuntu-rc",
		comp("ubuntu-rc", "apt", "amd64", "pkg", "1.0.9-1", day("2026-09-01")),
		comp("ubuntu-rc", "apt", "amd64", "pkg", "1.0.10-1", day("2026-09-02")),
	)
	pl := plan(t, api, Policy{Name: "k", Repositories: []string{"ubuntu-rc"}, KeepLatest: 1})
	assertDeleted(t, pl, "amd64/1.0.9-1")
}

func TestPlan_ExplicitVersionOrder(t *testing.T) {
	api := newFake()
	api.add("generic",
		comp("generic", "raw", "", "f", "1.0.0", day("2026-09-01")),
		comp("generic", "raw", "", "f", "1.0.0-rc.1", day("2026-09-01")),
	)
	// raw -> lexical, where the longer string sorts higher; forcing semver
	// makes the release newer than its release candidate.
	lex := plan(t, api, Policy{Name: "k", Repositories: []string{"generic"}, KeepLatest: 1})
	assertDeleted(t, lex, "/1.0.0")
	sem := plan(t, api, Policy{Name: "k", Repositories: []string{"generic"}, KeepLatest: 1, VersionOrder: "semver"})
	assertDeleted(t, sem, "/1.0.0-rc.1")
}

func TestPlan_ReleaseTypePrereleaseForMavenSnapshots(t *testing.T) {
	build := func() *fakeAPI {
		api := newFake()
		const repo = "maven-all"
		recent := day("2026-09-10")
		api.add(repo,
			comp(repo, "maven2", "com.acme", "lib", "1.0.0", recent),
			comp(repo, "maven2", "com.acme", "lib", "1.1.0", recent),
			comp(repo, "maven2", "com.acme", "lib", "1.2.0-SNAPSHOT", recent),
			comp(repo, "maven2", "com.acme", "lib", "1.2.0-20260101.000000-1", recent),
			comp(repo, "maven2", "com.acme", "lib", "1.2.0-20260301.000000-2", recent),
			comp(repo, "maven2", "com.acme", "lib", "1.2.0-20260901.000000-3", recent),
		)
		return api
	}
	// Only snapshots take part: keep the newest 1 of them. The releases do not
	// occupy slots nor get deleted. 1.2.0-SNAPSHOT sorts below the timestamped
	// builds in Maven order, so it goes too.
	pl := plan(t, build(), Policy{Name: "snap", Repositories: []string{"maven-all"}, KeepLatest: 1, ReleaseType: "prerelease"})
	got := deletedVersions(pl)
	for _, v := range got {
		if !strings.Contains(v, "1.2.0-") {
			t.Errorf("release deleted by prerelease policy: %v", got)
		}
	}
	if slices.Contains(got, "com.acme/1.2.0-20260901.000000-3") {
		t.Errorf("newest snapshot deleted: %v", got)
	}
	if len(got) != 3 {
		t.Errorf("deleted = %v, want 3 snapshots", got)
	}

	// release policy with keepLatest 1: only releases are candidates; snapshots are untouched.
	pl = plan(t, build(), Policy{Name: "rel", Repositories: []string{"maven-all"}, KeepLatest: 1, ReleaseType: "release"})
	assertDeleted(t, pl, "com.acme/1.0.0")
}

func TestPlan_FilteredComponentsDoNotCountTowardTopN(t *testing.T) {
	api := newFake()
	const repo = "m"
	old := day("2025-01-01")
	api.add(repo,
		comp(repo, "maven2", "g", "a", "1.0.0", old),
		comp(repo, "maven2", "g", "a", "2.0.0", old),
		comp(repo, "maven2", "g", "a", "3.0.0-SNAPSHOT", old),
		comp(repo, "maven2", "g", "a", "4.0.0-SNAPSHOT", old),
	)
	pl := plan(t, api, Policy{Name: "rel", Repositories: []string{repo}, KeepLatest: 1, ReleaseType: "release"})
	// Snapshots are newer but filtered out, so 2.0.0 is rank 1 and 1.0.0 is deleted.
	assertDeleted(t, pl, "g/1.0.0")
}

func TestPlan_IncludeExcludeNames(t *testing.T) {
	api := newFake()
	const repo = "r"
	old := day("2025-01-01")
	for _, n := range []string{"zextras-core", "zextras-core-dbg", "other"} {
		api.add(repo, comp(repo, "apt", "amd64", n, "1.0-1", old), comp(repo, "apt", "amd64", n, "2.0-1", old))
	}
	pl := plan(t, api, Policy{
		Name: "n", Repositories: []string{repo}, KeepLatest: 1,
		IncludeNames: []string{"^zextras-"}, ExcludeNames: []string{"-dbg$"},
	})
	if len(pl.Items) != 1 || pl.Items[0].Name != "zextras-core" || pl.Items[0].Version != "1.0-1" {
		t.Errorf("items = %+v", pl.Items)
	}
}

func TestPlan_ProtectVersions(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64", "2025-01-01", "2025-01-02", "2025-01-03", "2025-01-04")...)
	pl := plan(t, api, Policy{
		Name: "p", Repositories: []string{"ubuntu-rc"}, KeepLatest: 1,
		ProtectVersions: []string{`^1\.0\.1-`},
	})
	assertDeleted(t, pl, amd(0), amd(2)) // v1 protected; v3 is the newest
}

func TestPlan_ProtectedVersionStillOccupiesSlot(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64", "2025-01-01", "2025-01-02", "2025-01-03")...)
	pl := plan(t, api, Policy{
		Name: "p", Repositories: []string{"ubuntu-rc"}, KeepLatest: 2,
		ProtectVersions: []string{`^1\.0\.2-`}, // the newest, rank 1
	})
	assertDeleted(t, pl, amd(0))
}

func TestPlan_MultiPolicyDedupe(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64", "2025-01-01", "2025-01-02", "2025-01-03")...)
	pl := plan(t, api,
		Policy{Name: "first", Repositories: []string{"ubuntu-rc", "ubuntu-rc"}, KeepLatest: 2},
		Policy{Name: "second", Repositories: []string{"ubuntu-rc"}, KeepLatest: 1},
	)
	if len(pl.Items) != 2 {
		t.Fatalf("items = %+v", pl.Items)
	}
	byVer := map[string]string{}
	for _, it := range pl.Items {
		byVer[it.Version] = it.Policy
	}
	if byVer[debVer(0)] != "first" || byVer[debVer(1)] != "second" {
		t.Errorf("policy attribution = %v", byVer)
	}
	// Each repository is listed once even though two policies (and a repeated
	// entry) name it.
	if api.listed["ubuntu-rc"] != 1 {
		t.Errorf("listed %d times", api.listed["ubuntu-rc"])
	}
	if pl.Scanned != 3 || pl.Kept != 1 {
		t.Errorf("scanned/kept = %d/%d", pl.Scanned, pl.Kept)
	}
}

func TestPlan_AgeFromVersionTimestampVsUploaded(t *testing.T) {
	migrated := day("2026-06-03") // migration date: 110 days before testNow
	build := func() *fakeAPI {
		api := newFake()
		api.add("apt", // all uploaded on migration day
			comp("apt", "apt", "amd64", "zx", "0.10.9-20251104144405ubuntu", migrated),  // built 321d ago
			comp("apt", "apt", "amd64", "zx", "0.10.10-20260910000000ubuntu", migrated), // built 11d ago
			comp("apt", "apt", "amd64", "zx", "0.10.11-1ubuntu", migrated),              // no timestamp -> uploaded
		)
		return api
	}
	pol := Policy{Name: "age", Repositories: []string{"apt"}, OlderThan: 90}

	ts := plan(t, build(), pol) // default: versionTimestamp
	assertDeleted(t, ts, "amd64/0.10.11-1ubuntu", "amd64/0.10.9-20251104144405ubuntu")
	for _, it := range ts.Items {
		if it.Version == "0.10.9-20251104144405ubuntu" && it.Reason != "age 321d > 90d" {
			t.Errorf("reason = %q", it.Reason)
		}
	}

	pol.AgeFrom = "versionTimestamp"
	assertDeleted(t, plan(t, build(), pol), "amd64/0.10.11-1ubuntu", "amd64/0.10.9-20251104144405ubuntu")

	pol.AgeFrom = "uploaded"
	assertDeleted(t, plan(t, build(), pol),
		"amd64/0.10.10-20260910000000ubuntu", "amd64/0.10.11-1ubuntu", "amd64/0.10.9-20251104144405ubuntu")
}

func TestPlan_AgeFallbackToLastModifiedAndUnknown(t *testing.T) {
	lm := day("2025-01-01")
	size := int64(5)
	c := comp("r", "apt", "amd64", "a", "1-1", time.Time{})
	c.Assets[0] = v3.AssetXO{LastModified: &lm, FileSize: &size}
	unknown := comp("r", "apt", "amd64", "b", "1-1", time.Time{})
	unknown.Assets = nil
	api := newFake()
	api.add("r", c, unknown)

	pl := plan(t, api, Policy{Name: "o", Repositories: []string{"r"}, OlderThan: 30})
	if len(pl.Items) != 1 || pl.Items[0].Name != "a" || pl.Items[0].Size != 5 {
		t.Errorf("items = %+v (component of unknown age must be kept)", pl.Items)
	}

	// Unknown age is also rescued from keepLatest when keepDays is set.
	api2 := newFake()
	api2.add("r",
		comp("r", "apt", "amd64", "b", "2-1", day("2025-01-01")),
		func() v3.ComponentXO {
			u := comp("r", "apt", "amd64", "b", "1-1", time.Time{})
			u.Assets = nil
			return u
		}(),
	)
	pl = plan(t, api2, Policy{Name: "o", Repositories: []string{"r"}, KeepLatest: 1, KeepDays: new(3)})
	assertDeleted(t, pl)
	pl = plan(t, api2, Policy{Name: "o", Repositories: []string{"r"}, KeepLatest: 1})
	assertDeleted(t, pl, "amd64/1-1")
}

func TestPlan_Totals(t *testing.T) {
	api := newFake()
	var comps []v3.ComponentXO
	for i := range 250 {
		comps = append(comps, comp("big", "apt", "amd64", "pkg", fmt.Sprintf("1.%d-1", i), day("2025-01-01")))
	}
	api.add("big", comps...)
	api.add("empty")
	pl := plan(t, api,
		Policy{Name: "a", Repositories: []string{"big", "empty"}, KeepLatest: 200},
	)
	if pl.Scanned != 250 || len(pl.Items) != 50 || pl.Kept != 200 {
		t.Errorf("scanned/items/kept = %d/%d/%d", pl.Scanned, len(pl.Items), pl.Kept)
	}
	if pl.Bytes != 50*1000 {
		t.Errorf("bytes = %d", pl.Bytes)
	}
	// big: ceil(250/100)=3, empty: min 1, deletes: 50.
	if pl.EstimatedRequests != 3+1+50 {
		t.Errorf("estimated requests = %d", pl.EstimatedRequests)
	}
}

func TestPlan_Errors(t *testing.T) {
	e := NewEngine(newFake(), Options{})
	if _, err := e.Plan(context.Background(), nil); err == nil {
		t.Error("nil config: expected error")
	}
	if _, err := e.Plan(context.Background(), &Config{}); err == nil {
		t.Error("empty config: expected error")
	}
	if _, err := e.Plan(context.Background(), &Config{Policies: []Policy{{Name: "x", Repositories: []string{"r"}}}}); err == nil ||
		!strings.Contains(err.Error(), `policy[0] "x"`) {
		t.Errorf("invalid policy: %v", err)
	}

	api := newFake()
	boom := errors.New("boom")
	api.add("r", debBuilds("amd64", "2025-01-01")...)
	api.listErr["r"] = boom
	_, err := NewEngine(api, Options{}).Plan(context.Background(), &Config{Policies: []Policy{{Name: "x", Repositories: []string{"r"}, KeepLatest: 1}}})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), `"r"`) {
		t.Errorf("list error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api = newFake()
	api.add("r", debBuilds("amd64", "2025-01-01")...)
	_, err = NewEngine(api, Options{}).Plan(ctx, &Config{Policies: []Policy{{Name: "x", Repositories: []string{"r"}, KeepLatest: 1}}})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled plan error = %v", err)
	}
}

func TestPlan_DefaultClockAndSkipsComponentsWithoutID(t *testing.T) {
	api := newFake()
	noID := comp("r", "apt", "amd64", "a", "1-1", day("2000-01-01"))
	noID.Id = nil
	api.add("r", noID, comp("r", "apt", "amd64", "a", "2-1", day("2000-01-01")))
	pl, err := NewEngine(api, Options{}).Plan(context.Background(), &Config{Policies: []Policy{{Name: "x", Repositories: []string{"r"}, OlderThan: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.Items) != 1 || pl.Items[0].Version != "2-1" {
		t.Errorf("items = %+v", pl.Items)
	}
}

func TestPlan_JSON(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64", "2025-01-01", "2025-01-02")...)
	pl := plan(t, api, Policy{Name: "k", Repositories: []string{"ubuntu-rc"}, KeepLatest: 1})
	b, err := json.Marshal(pl)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Items []map[string]any `json:"items"`
		Kept  int              `json:"kept"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0]["policy"] != "k" || got.Items[0]["ageDays"] != float64(628) || got.Items[0]["version"] != debVer(0) {
		t.Errorf("json = %s", b)
	}

	rb, err := json.Marshal([]Result{{Item: pl.Items[0]}, {Item: pl.Items[0], Err: ErrRequestBudget}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rb), `"error":"cleanup: request budget exhausted"`) || strings.Count(string(rb), `"error"`) != 1 {
		t.Errorf("results json = %s", rb)
	}

	empty, _ := json.Marshal(plan(t, newFake(), Policy{Name: "k", Repositories: []string{"none"}, KeepLatest: 1}))
	if !strings.Contains(string(empty), `"items":[]`) {
		t.Errorf("empty plan json = %s", empty)
	}
}

func TestCalendarDays(t *testing.T) {
	now := time.Date(2026, 9, 21, 0, 0, 1, 0, time.UTC)
	tests := []struct {
		ts   time.Time
		want int
	}{
		{time.Date(2026, 9, 21, 23, 59, 0, 0, time.UTC), 0},
		{time.Date(2026, 9, 20, 23, 59, 59, 0, time.UTC), 1},
		{time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), 14},
		{time.Date(2026, 9, 6, 23, 59, 59, 0, time.UTC), 15},
		{time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC), -1},
		// Offsets are normalised to UTC: 2026-09-21T01:00+05:00 is 09-20 20:00Z.
		{time.Date(2026, 9, 21, 1, 0, 0, 0, time.FixedZone("x", 5*3600)), 1},
	}
	for _, tt := range tests {
		if got := calendarDays(now, tt.ts); got != tt.want {
			t.Errorf("calendarDays(%v) = %d, want %d", tt.ts, got, tt.want)
		}
	}
}

// --- Apply ------------------------------------------------------------------

// applyPlan builds a plan of n deletable items on a fresh fake.
func applyPlan(t *testing.T, n int) (*fakeAPI, *Plan) {
	t.Helper()
	api := newFake()
	var comps []v3.ComponentXO
	for i := range n + 1 {
		comps = append(comps, comp("r", "apt", "amd64", "pkg", fmt.Sprintf("1.%03d-1", i), day("2025-01-01")))
	}
	api.add("r", comps...)
	pl := plan(t, api, Policy{Name: "k", Repositories: []string{"r"}, KeepLatest: 1})
	if len(pl.Items) != n {
		t.Fatalf("plan has %d items, want %d", len(pl.Items), n)
	}
	return api, pl
}

func TestApply_DeletesAllInPlanOrder(t *testing.T) {
	api, pl := applyPlan(t, 25)
	var inflight, maxInflight atomic.Int32
	api.onDelete = func(context.Context, string) error {
		n := inflight.Add(1)
		for {
			m := maxInflight.Load()
			if n <= m || maxInflight.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		inflight.Add(-1)
		return nil
	}
	res := NewEngine(api, Options{}).Apply(context.Background(), pl, 4)
	if len(res) != 25 {
		t.Fatalf("results = %d", len(res))
	}
	for i, r := range res {
		if r.Err != nil || r.Item.ID != pl.Items[i].ID {
			t.Errorf("result[%d] = %+v", i, r)
		}
	}
	if len(api.deleted) != 25 {
		t.Errorf("deleted %d", len(api.deleted))
	}
	if m := maxInflight.Load(); m > 4 || m < 2 {
		t.Errorf("max in-flight deletes = %d, want 2..4", m)
	}
}

func TestApply_ConcurrencyBelowOneMeansOne(t *testing.T) {
	api, pl := applyPlan(t, 5)
	var inflight atomic.Int32
	api.onDelete = func(context.Context, string) error {
		if inflight.Add(1) != 1 {
			t.Error("concurrent delete with concurrency 0")
		}
		time.Sleep(time.Millisecond)
		inflight.Add(-1)
		return nil
	}
	for _, c := range []int{0, -3} {
		api.deleted = nil
		res := NewEngine(api, Options{}).Apply(context.Background(), pl, c)
		if len(res) != 5 || len(api.deleted) != 5 {
			t.Errorf("concurrency %d: results=%d deleted=%d", c, len(res), len(api.deleted))
		}
	}
}

func TestApply_PartialFailureDoesNotAbort(t *testing.T) {
	api, pl := applyPlan(t, 6)
	boom := errors.New("403 forbidden")
	api.failIDs[pl.Items[1].ID] = boom
	api.failIDs[pl.Items[4].ID] = boom
	res := NewEngine(api, Options{}).Apply(context.Background(), pl, 3)
	var failed int
	for i, r := range res {
		wantErr := i == 1 || i == 4
		if (r.Err != nil) != wantErr {
			t.Errorf("result[%d].Err = %v", i, r.Err)
		}
		if r.Err != nil {
			failed++
			if !errors.Is(r.Err, boom) {
				t.Errorf("result[%d].Err = %v", i, r.Err)
			}
		}
	}
	if failed != 2 || len(api.deleted) != 4 {
		t.Errorf("failed=%d deleted=%d", failed, len(api.deleted))
	}
}

func TestApply_ContextCancellation(t *testing.T) {
	api, pl := applyPlan(t, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	api.onDelete = func(context.Context, string) error {
		if calls.Add(1) == 3 {
			cancel()
		}
		return nil
	}
	res := NewEngine(api, Options{}).Apply(ctx, pl, 1)
	if len(res) != 10 {
		t.Fatalf("results = %d", len(res))
	}
	for i, r := range res {
		switch {
		case i < 3 && r.Err != nil:
			t.Errorf("result[%d] = %v, want success", i, r.Err)
		case i >= 3 && !errors.Is(r.Err, context.Canceled):
			t.Errorf("result[%d] = %v, want context.Canceled", i, r.Err)
		}
	}
	if len(api.deleted) != 3 {
		t.Errorf("deleted %d, want 3", len(api.deleted))
	}
}

func TestApply_AlreadyCancelled(t *testing.T) {
	api, pl := applyPlan(t, 4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := NewEngine(api, Options{}).Apply(ctx, pl, 4)
	for i, r := range res {
		if !errors.Is(r.Err, context.Canceled) {
			t.Errorf("result[%d] = %v", i, r.Err)
		}
	}
	if len(api.deleted) != 0 {
		t.Errorf("deleted %d", len(api.deleted))
	}
}

func TestApply_RequestBudget(t *testing.T) {
	tests := []struct {
		name        string
		max         int
		wantDeleted int
	}{
		{"unlimited", 0, 8},
		{"exactly enough", 1 + 8, 8},
		{"three deletes fit", 1 + 3, 3},
		{"only listing fits", 1, 0},
		{"listing alone exceeds budget", 0 + 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, pl := applyPlan(t, 8) // 9 components: 1 list request + 8 deletes
			if pl.EstimatedRequests != 9 {
				t.Fatalf("estimated = %d", pl.EstimatedRequests)
			}
			res := NewEngine(api, Options{MaxRequests: tt.max}).Apply(context.Background(), pl, 2)
			var ok, budget int
			for i, r := range res {
				switch {
				case r.Err == nil:
					ok++
				case errors.Is(r.Err, ErrRequestBudget):
					budget++
					if i < tt.wantDeleted {
						t.Errorf("result[%d] budget-limited too early", i)
					}
				default:
					t.Errorf("result[%d] = %v", i, r.Err)
				}
			}
			if ok != tt.wantDeleted || ok+budget != 8 || len(api.deleted) != tt.wantDeleted {
				t.Errorf("ok=%d budget=%d deleted=%d, want %d deleted", ok, budget, len(api.deleted), tt.wantDeleted)
			}
		})
	}
}

func TestApply_BudgetSmallerThanListingCost(t *testing.T) {
	api, pl := applyPlan(t, 3)
	res := NewEngine(api, Options{MaxRequests: 0 + 1}).Apply(context.Background(), &Plan{Items: pl.Items, EstimatedRequests: 50 + 3}, 2)
	for i, r := range res {
		if !errors.Is(r.Err, ErrRequestBudget) {
			t.Errorf("result[%d] = %v", i, r.Err)
		}
	}
	if len(api.deleted) != 0 {
		t.Errorf("deleted %d", len(api.deleted))
	}
}

func TestApply_NilAndEmptyPlan(t *testing.T) {
	e := NewEngine(newFake(), Options{})
	if res := e.Apply(context.Background(), nil, 2); res != nil {
		t.Errorf("nil plan: %v", res)
	}
	if res := e.Apply(context.Background(), &Plan{}, 2); len(res) != 0 {
		t.Errorf("empty plan: %v", res)
	}
}

func TestApply_ThenPlanEndToEnd(t *testing.T) {
	api := newFake()
	api.add("ubuntu-rc", debBuilds("amd64", "2025-01-01", "2025-01-02", "2025-01-03")...)
	e := NewEngine(api, Options{Now: func() time.Time { return testNow }})
	pl, err := e.Plan(context.Background(), &Config{Policies: []Policy{{Name: "k", Repositories: []string{"ubuntu-rc"}, KeepLatest: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	res := e.Apply(context.Background(), pl, 2)
	for _, r := range res {
		if r.Err != nil {
			t.Errorf("%+v", r)
		}
	}
	slices.Sort(api.deleted)
	want := []string{"ubuntu-rc|amd64|pkg|" + debVer(0), "ubuntu-rc|amd64|pkg|" + debVer(1)}
	if !slices.Equal(api.deleted, want) {
		t.Errorf("deleted = %v, want %v", api.deleted, want)
	}
}
