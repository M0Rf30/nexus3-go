package cleanup

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"slices"
	"time"

	v3 "github.com/sonatype-nexus-community/nexus-repo-api-client-go/v3"

	"github.com/M0Rf30/nexus3-go/pkg/nexus3/cleanup/version"
)

// pageSize is the number of components Nexus returns per list request; it is
// used only to estimate how many requests listing a repository costs.
const pageSize = 100

// ErrRequestBudget is the error recorded in Result.Err for items that were
// not deleted because Options.MaxRequests was exhausted.
var ErrRequestBudget = errors.New("cleanup: request budget exhausted")

// API is the subset of the Nexus client the engine needs. *nexus3.Client
// satisfies it.
type API interface {
	// AllComponents iterates over every component in repository.
	AllComponents(ctx context.Context, repository string) iter.Seq2[v3.ComponentXO, error]
	// DeleteComponent deletes the component with the given id.
	DeleteComponent(ctx context.Context, id string) error
}

// Options tunes an Engine.
type Options struct {
	// Now returns the current time; nil means time.Now. Tests inject a fixed
	// clock to make age arithmetic deterministic.
	Now func() time.Time
	// MaxRequests caps the HTTP requests a run (Plan listing plus Apply
	// deletes) may spend; 0 means unlimited. Requests are estimated as one
	// per 100 components listed (rounded up, at least one per repository)
	// plus one per delete. Plan always lists everything; the cap is enforced
	// by Apply, which deletes only as many items as the remaining budget
	// allows and marks the rest ErrRequestBudget.
	MaxRequests int
}

// Item is one component the plan proposes to delete.
type Item struct {
	// Policy is the name of the policy that selected the component.
	Policy string `json:"policy"`
	// Repository is the repository holding the component.
	Repository string `json:"repository"`
	// ID is the Nexus component id passed to DeleteComponent.
	ID string `json:"id"`
	// Group is the component group (architecture for apt/yum, groupId for
	// maven2).
	Group string `json:"group"`
	// Name is the component name.
	Name string `json:"name"`
	// Version is the component version.
	Version string `json:"version"`
	// Reason explains in human-readable form why the component is deleted,
	// e.g. "rank 5 > keepLatest 3 and age 120d > 90d".
	Reason string `json:"reason"`
	// Size is the total size in bytes of the component's assets.
	Size int64 `json:"size"`
	// Age is the component age at planning time (zero when unknown).
	Age time.Duration `json:"-"`
}

// MarshalJSON encodes the item with its age expressed in whole days
// ("ageDays") instead of a nanosecond duration.
//
//nolint:gocritic // value receiver so Item values marshal without being addressable
func (i Item) MarshalJSON() ([]byte, error) {
	type plain Item
	return json.Marshal(struct {
		plain
		AgeDays int `json:"ageDays"`
	}{plain(i), int(i.Age / (24 * time.Hour))})
}

// Plan is a snapshot of what a cleanup run would delete. It is computed once
// from the repository listing and is not re-validated before Apply: anything
// uploaded, deleted or downloaded in between is not taken into account.
type Plan struct {
	// Items are the components to delete, ordered by policy, repository,
	// group, name and then newest version first.
	Items []Item `json:"items"`
	// Scanned is the number of components listed across all repositories.
	Scanned int `json:"scanned"`
	// Kept is Scanned minus len(Items): everything the plan leaves alone,
	// including components outside every policy's scope.
	Kept int `json:"kept"`
	// EstimatedRequests is the estimated HTTP request count of the whole run:
	// listing pages plus one delete per item.
	EstimatedRequests int `json:"estimatedRequests"`
	// Bytes is the total size of the components in Items.
	Bytes int64 `json:"bytes"`
}

// Result is the outcome of applying one plan Item.
type Result struct {
	// Item is the planned deletion this result belongs to.
	Item Item `json:"item"`
	// Err is nil on success, ErrRequestBudget when the budget ran out, the
	// context error when the run was cancelled, or the API error.
	Err error `json:"-"`
}

// MarshalJSON encodes the result with the error rendered as a string
// ("error", omitted on success).
//
//nolint:gocritic // value receiver so Result values marshal without being addressable
func (r Result) MarshalJSON() ([]byte, error) {
	out := struct {
		Item  Item   `json:"item"`
		Error string `json:"error,omitempty"`
	}{Item: r.Item}
	if r.Err != nil {
		out.Error = r.Err.Error()
	}
	return json.Marshal(out)
}

// Engine plans and applies cleanup policies against a Nexus server.
type Engine struct {
	api  API
	opts Options
}

// NewEngine returns an Engine that talks to api.
func NewEngine(api API, opts Options) *Engine {
	return &Engine{api: api, opts: opts}
}

func (e *Engine) now() time.Time {
	if e.opts.Now != nil {
		return e.opts.Now()
	}
	return time.Now()
}

// candidate is a component reduced to what the decision logic needs.
type candidate struct {
	id, group, name, version string
	size                     int64
	ts                       time.Time // age reference; valid only if hasTS
	hasTS                    bool
}

// Plan lists every repository named by cfg (each at most once, even when
// several policies share it) and returns the components to delete. It never
// modifies the server. A listing error aborts planning, since decisions on an
// incomplete group would be unsafe.
//
// See the package documentation for the decision rules.
func (e *Engine) Plan(ctx context.Context, cfg *Config) (*Plan, error) {
	if cfg == nil {
		return nil, errors.New("cleanup: plan: nil config")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	compiled := make([]*compiledPolicy, len(cfg.Policies))
	for i := range cfg.Policies {
		cp, err := compilePolicy(i, &cfg.Policies[i])
		if err != nil {
			return nil, err
		}
		compiled[i] = cp
	}

	now := e.now()
	plan := &Plan{Items: []Item{}}
	listed := make(map[string][]v3.ComponentXO)
	planned := make(map[string]struct{})

	for _, cp := range compiled {
		done := make(map[string]struct{}, len(cp.Repositories))
		for _, repo := range cp.Repositories {
			if _, dup := done[repo]; dup {
				continue
			}
			done[repo] = struct{}{}

			comps, ok := listed[repo]
			if !ok {
				var err error
				if comps, err = e.list(ctx, repo); err != nil {
					return nil, err
				}
				listed[repo] = comps
				plan.Scanned += len(comps)
				plan.EstimatedRequests += listRequests(len(comps))
			}
			found := evaluate(cp, repo, comps, now)
			for i := range found {
				it := found[i]
				if _, dup := planned[it.ID]; dup {
					continue
				}
				planned[it.ID] = struct{}{}
				plan.Items = append(plan.Items, it)
				plan.Bytes += it.Size
			}
		}
	}
	plan.Kept = plan.Scanned - len(plan.Items)
	plan.EstimatedRequests += len(plan.Items)
	return plan, nil
}

func listRequests(n int) int {
	return max(1, (n+pageSize-1)/pageSize)
}

func (e *Engine) list(ctx context.Context, repo string) ([]v3.ComponentXO, error) {
	var out []v3.ComponentXO
	for c, err := range e.api.AllComponents(ctx, repo) {
		if err != nil {
			return nil, fmt.Errorf("cleanup: list repository %q: %w", repo, err)
		}
		out = append(out, c)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cleanup: list repository %q: %w", repo, err)
	}
	return out, nil
}

// evaluate applies one policy to the components of one repository and
// returns the items to delete.
func evaluate(cp *compiledPolicy, repo string, comps []v3.ComponentXO, now time.Time) []Item {
	type groupKey struct{ group, name string }
	groups := make(map[groupKey][]candidate)
	formats := make(map[groupKey]string)

	for i := range comps {
		c := &comps[i]
		id := c.GetId()
		if id == "" {
			continue // cannot be deleted without an id
		}
		name, ver := c.GetName(), c.GetVersion()
		if len(cp.include) > 0 && !matchAny(cp.include, name) {
			continue
		}
		if matchAny(cp.exclude, name) {
			continue
		}
		scheme := cp.schemeFor(c.GetFormat())
		if cp.releaseOnly || cp.prereleaseOnly {
			if pre := version.IsPrerelease(scheme, ver); pre != cp.prereleaseOnly {
				continue
			}
		}
		k := groupKey{c.GetGroup(), name}
		cand := candidate{id: id, group: k.group, name: name, version: ver}
		cand.size, cand.ts, cand.hasTS = componentStats(c, cp.fromTS)
		groups[k] = append(groups[k], cand)
		if _, ok := formats[k]; !ok {
			formats[k] = c.GetFormat()
		}
	}

	keys := make([]groupKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b groupKey) int {
		return cmp.Or(cmp.Compare(a.group, b.group), cmp.Compare(a.name, b.name))
	})

	var items []Item
	for _, k := range keys {
		scheme := cp.schemeFor(formats[k])
		g := groups[k]
		slices.SortFunc(g, func(a, b candidate) int {
			return cmp.Or(
				version.Compare(scheme, b.version, a.version), // newest first
				compareTimeDesc(&a, &b),
				cmp.Compare(a.id, b.id),
			)
		})
		for idx := range g {
			cand := g[idx]
			reason, del := cp.decide(idx+1, &cand, now)
			if !del {
				continue
			}
			it := Item{
				Policy:     cp.Name,
				Repository: repo,
				ID:         cand.id,
				Group:      cand.group,
				Name:       cand.name,
				Version:    cand.version,
				Reason:     reason,
				Size:       cand.size,
			}
			if cand.hasTS {
				it.Age = max(0, now.Sub(cand.ts))
			}
			items = append(items, it)
		}
	}
	return items
}

func compareTimeDesc(a, b *candidate) int {
	if a.hasTS && b.hasTS {
		return b.ts.Compare(a.ts)
	}
	return 0
}

// schemeFor returns the version scheme for a component of the given format.
func (cp *compiledPolicy) schemeFor(format string) version.Scheme {
	if cp.auto {
		return version.SchemeForFormat(format)
	}
	return cp.scheme
}

// decide reports whether the candidate at the given 1-based rank (newest
// first) is to be deleted, with a human-readable reason.
func (cp *compiledPolicy) decide(rank int, c *candidate, now time.Time) (string, bool) {
	if matchAny(cp.protect, c.version) {
		return "", false
	}

	var ageDays int
	if c.hasTS {
		ageDays = calendarDays(now, c.ts)
	}

	outside := cp.KeepLatest > 0 && rank > cp.KeepLatest
	older := cp.OlderThan > 0 && c.hasTS && ageDays > int(cp.OlderThan)

	var reason string
	switch {
	case cp.KeepLatest > 0 && cp.OlderThan > 0:
		if !outside || !older {
			return "", false
		}
		reason = fmt.Sprintf("rank %d > keepLatest %d and age %dd > %s", rank, cp.KeepLatest, ageDays, cp.OlderThan)
	case cp.KeepLatest > 0:
		if !outside {
			return "", false
		}
		reason = fmt.Sprintf("rank %d > keepLatest %d", rank, cp.KeepLatest)
	default:
		if !older {
			return "", false
		}
		reason = fmt.Sprintf("age %dd > %s", ageDays, cp.OlderThan)
	}

	if cp.KeepDays != nil {
		// Union keep: recent components survive regardless of rank. A
		// component of unknown age cannot be shown to be stale, so it is
		// kept too.
		if !c.hasTS || ageDays <= *cp.KeepDays {
			return "", false
		}
		reason += fmt.Sprintf(" and age %dd > keepDays %d", ageDays, *cp.KeepDays)
	}
	return reason, true
}

// calendarDays returns the number of UTC calendar days between ts and now
// (0 for the same day, negative for the future), so that "keepDays: 0" means
// "built today" and the cutoff date itself is inclusive.
func calendarDays(now, ts time.Time) int {
	n, t := now.UTC(), ts.UTC()
	nd := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	td := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return int(nd.Sub(td).Hours() / 24)
}

// componentStats sums the asset sizes and picks the age reference time: the
// version's embedded build time when fromVersion is set and present,
// otherwise the newest asset BlobCreated, otherwise the newest LastModified.
func componentStats(c *v3.ComponentXO, fromVersion bool) (size int64, ts time.Time, ok bool) {
	var blobCreated, lastModified time.Time
	for i := range c.Assets {
		a := &c.Assets[i]
		size += a.GetFileSize()
		if t := a.GetBlobCreated(); t.After(blobCreated) {
			blobCreated = t
		}
		if t := a.GetLastModified(); t.After(lastModified) {
			lastModified = t
		}
	}
	if fromVersion {
		if t, found := version.BuildTime(c.GetVersion()); found {
			return size, t, true
		}
	}
	switch {
	case !blobCreated.IsZero():
		return size, blobCreated, true
	case !lastModified.IsZero():
		return size, lastModified, true
	}
	return size, time.Time{}, false
}

// Apply deletes the items of plan with at most concurrency parallel requests
// (values below 1 mean 1) and returns one Result per item, in plan order.
//
// A failed delete never stops the run. When ctx is cancelled, items not yet
// started get ctx.Err(). When Options.MaxRequests is set, the requests Plan
// spent listing (EstimatedRequests minus one per item) are subtracted and
// only as many leading items as still fit are attempted; the rest get
// ErrRequestBudget. Apply trusts the plan snapshot and does not re-check
// components before deleting them.
func (e *Engine) Apply(ctx context.Context, plan *Plan, concurrency int) []Result {
	if plan == nil {
		return nil
	}
	items := plan.Items
	results := make([]Result, len(items))
	for i := range items {
		results[i].Item = items[i]
	}

	attempt := len(items)
	if e.opts.MaxRequests > 0 {
		listing := max(0, plan.EstimatedRequests-len(items))
		attempt = min(attempt, max(0, e.opts.MaxRequests-listing))
		for i := attempt; i < len(items); i++ {
			results[i].Err = ErrRequestBudget
		}
	}

	concurrency = max(1, min(concurrency, max(1, attempt)))
	jobs := make(chan int)
	done := make(chan struct{})
	for range concurrency {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := range jobs {
				if err := ctx.Err(); err != nil {
					results[i].Err = err
					continue
				}
				results[i].Err = e.api.DeleteComponent(ctx, items[i].ID)
			}
		}()
	}
	for i := range attempt {
		jobs <- i
	}
	close(jobs)
	for range concurrency {
		<-done
	}
	return results
}
