// Package cleanup implements client-side retention policies for Nexus
// Repository Manager 3 Community Edition, which lacks the Pro-only "retain N
// versions" cleanup. It is modelled on zextras' JFrog cleanup
// (infra-jfrog-cleanup): list the components of a repository, group them,
// order every group newest-first and delete what the policy does not keep.
//
// # Workflow
//
// Load a [Config] with [LoadConfig] or [ParseConfig], build an [Engine] with
// [NewEngine] (a *nexus3.Client satisfies [API]), compute a [Plan] with
// [Engine.Plan] (read-only) and, once reviewed, run [Engine.Apply]. The plan
// is a snapshot: nothing is re-fetched before deleting, so apply it soon
// after planning. Deleting components only marks their blobs for removal; run
// a blob store compact task to reclaim disk space.
//
// # Configuration
//
// A policy applies to one or more repositories. The example below is the
// equivalent of the zextras "ubuntu-rc" rule, which keeps every build of the
// last 14 days or the 3 newest builds of each package and architecture,
// whichever keeps more, and never touches releases:
//
//	policies:
//	  - name: ubuntu-rc
//	    repositories: [ubuntu-rc]
//	    keepLatest: 3
//	    keepDays: 14
//	    releaseType: any
//	  - name: maven-snapshots
//	    repositories: [maven-snapshots]
//	    olderThan: 90d
//	    releaseType: prerelease
//	    protectVersions: ["^1\\.4\\.0-"]
//
// Unknown keys are rejected. A policy needs a name, at least one repository
// and at least one of keepLatest or olderThan; see [Policy] for every field.
//
// # Grouping and ordering
//
// Components are grouped per repository, component group and name. Nexus
// stores the architecture in the group for apt and yum, so amd64 and arm64
// builds of a package are independent groups, each keeping its own newest
// versions (like the per-architecture grouping of the zextras rule); for
// maven2 the group is the groupId. Within a group versions are ordered
// newest-first using the scheme of the component format (apt: Debian, yum:
// RPM, maven2: Maven, ...) or Policy.VersionOrder.
//
// # Age
//
// Content migrated into Nexus has an upload date equal to the migration
// date, so by default (ageFrom: versionTimestamp) the age comes from the
// build timestamp embedded in the version (for example the
// 20251104144405 in 0.10.9-20251104144405ubuntu) and only falls back to the
// newest asset BlobCreated, then LastModified, when the version has none.
// ageFrom: uploaded uses the asset timestamps only. Ages are whole UTC
// calendar days, so keepDays: 14 keeps everything built on or after the date
// 14 days ago and keepDays: 0 keeps today's builds. A component whose age
// cannot be determined is never deleted by an age-based rule.
//
// # Decision rules
//
// Components excluded by releaseType, includeNames or excludeNames are out of
// scope: they are never deleted by the policy and do not occupy keepLatest
// slots. Among the remaining components of a group, newest first with rank
// 1 the newest:
//
//   - keepLatest only: delete rank > keepLatest.
//   - olderThan only: delete age > olderThan.
//   - both: delete only when rank > keepLatest AND age > olderThan.
//   - keepDays, when set, rescues any component whose age is at most keepDays
//     (a union with the keepLatest set). Recent components still count
//     towards keepLatest, so an actively built package keeps exactly its
//     recent window rather than the window plus N stale extras, while a
//     dormant package keeps its N newest as a floor.
//   - a version matching protectVersions is never deleted, but still occupies
//     a keepLatest slot.
//
// A component selected by several policies appears once in the plan, under
// the first policy (in file order) that selects it. Every policy sees the
// full repository content, so two policies on one repository are independent.
//
// # Request budget
//
// [Options].MaxRequests bounds the HTTP requests of a run: one per 100
// components listed (at least one per repository) plus one per delete. The
// estimate is reported in [Plan].EstimatedRequests; [Engine.Apply] stops
// deleting when the budget is spent and marks the remaining results
// [ErrRequestBudget].
package cleanup
