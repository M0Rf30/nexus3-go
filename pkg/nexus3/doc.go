// Package nexus3 is a small, idiomatic wrapper around the community-generated
// Sonatype Nexus Repository Manager 3 OpenAPI client
// (github.com/sonatype-nexus-community/nexus-repo-api-client-go). It is
// unofficial and not affiliated with Sonatype.
//
// Uploads are one asset per request for every hosted format except maven2.
// UploadMaven2Assets packs up to MaxMaven2Assets sibling assets (e.g. a
// jar, its pom, and a sources jar) that share one set of coordinates into
// a single createComponents request, matching how Nexus itself limits a
// maven2 component upload to asset1..asset3. UploadMaven2 is a one-asset
// convenience wrapper around UploadMaven2Assets for the common case of
// uploading just one file.
//
// Breaking change from v0.3.0: Extension and Classifier moved off
// Maven2Coordinates onto Maven2Asset, since a multi-asset upload needs a
// distinct extension/classifier per asset rather than one shared by the
// whole component.
//
// UploadBatch runs a caller-supplied per-path upload function over many
// paths with bounded concurrency. Unlike a naive loop, it never aborts on
// the first error: it always attempts every path and returns one
// UploadResult per path (in input order) so a caller can report exactly
// which files failed alongside the ones that succeeded.
package nexus3
