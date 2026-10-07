package nexus3

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	v3 "github.com/sonatype-nexus-community/nexus-repo-api-client-go/v3"
)

// MaxMaven2Assets is the Nexus/SDK createComponents multipart limit for a
// single maven2 upload (asset1..asset3) — not a limit nexus3-go imposes.
const MaxMaven2Assets = 3

// simpleFormats are Nexus hosted-repository formats whose createComponents
// upload needs nothing beyond the file itself: one "<format>.asset" file
// part, no other required field. apt and yum need extra handling (yum
// requires an explicit filename part; apt gets its own named method for
// symmetry with UploadRpm) so they are not listed here.
var simpleFormats = map[string]func(v3.ApiCreateComponentsRequest, *os.File) v3.ApiCreateComponentsRequest{
	"go":   func(r v3.ApiCreateComponentsRequest, f *os.File) v3.ApiCreateComponentsRequest { return r.GoAsset(f) },
	"helm": func(r v3.ApiCreateComponentsRequest, f *os.File) v3.ApiCreateComponentsRequest { return r.HelmAsset(f) },
	"npm":  func(r v3.ApiCreateComponentsRequest, f *os.File) v3.ApiCreateComponentsRequest { return r.NpmAsset(f) },
	"nuget": func(r v3.ApiCreateComponentsRequest, f *os.File) v3.ApiCreateComponentsRequest {
		return r.NugetAsset(f)
	},
	"pypi": func(r v3.ApiCreateComponentsRequest, f *os.File) v3.ApiCreateComponentsRequest { return r.PypiAsset(f) },
	"rubygems": func(r v3.ApiCreateComponentsRequest, f *os.File) v3.ApiCreateComponentsRequest {
		return r.RubygemsAsset(f)
	},
}

// Maven2Coordinates identifies the maven2 component that UploadMaven2Assets
// (and its one-asset wrapper UploadMaven2) place one or more assets into.
// GroupID, ArtifactID, and Version are required by Nexus; Packaging and
// GeneratePOM are optional. Extension and Classifier live on Maven2Asset
// instead, since Nexus lets each asset in a multi-asset upload declare its
// own (e.g. a jar alongside its sources.jar and a pom).
type Maven2Coordinates struct {
	GroupID     string
	ArtifactID  string
	Version     string
	Packaging   string
	GeneratePOM bool
}

// Maven2Asset is one file within a maven2 component upload: its path on
// disk plus the per-asset Extension and Classifier Nexus uses to
// distinguish sibling assets sharing one set of Maven2Coordinates.
// Extension defaults server-side from the filename when empty.
type Maven2Asset struct {
	Path       string
	Extension  string
	Classifier string
}

func openUploadFile(op, filePath string) (*os.File, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("nexus3: upload %s %s: %w", op, filePath, err)
	}
	return f, nil
}

func (c *Client) doUpload(ctx context.Context, op, repository, filePath string, req *v3.ApiCreateComponentsRequest) error {
	resp, err := req.Repository(repository).Execute()
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return wrapAPIError(fmt.Sprintf("upload %s %s to %s", op, filePath, repository), resp, err)
	}
	return nil
}

// UploadDeb uploads a single .deb file to a Nexus apt (hosted) repository.
//
// It wraps ComponentsAPI.CreateComponents (POST /v1/components) with the
// apt.asset multipart field. Nexus's own OpenAPI/swagger.json declares no
// request body for createComponents — the multipart fields are undocumented
// there and only known because the generated Go SDK hard-codes them. That
// means uploads cannot be reached via the CLI's Restish-generated commands
// (which are driven entirely by the discoverable spec); this hand-wired call
// (and its siblings below) is the only way to perform them. Docker is the
// one common format NOT covered here: Nexus docker repositories use the
// separate Docker Registry HTTP API v2 ("docker push"), not this endpoint.
func (c *Client) UploadDeb(ctx context.Context, repository, filePath string) error {
	f, err := openUploadFile("deb", filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	req := c.api.ComponentsAPI.CreateComponents(c.authCtx(ctx)).AptAsset(f)
	return c.doUpload(ctx, "deb", repository, filePath, &req)
}

// UploadRpm uploads a single .rpm file to a Nexus yum (hosted) repository.
// See UploadDeb for why this bypasses the generic CLI path.
func (c *Client) UploadRpm(ctx context.Context, repository, filePath string) error {
	f, err := openUploadFile("rpm", filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	req := c.api.ComponentsAPI.CreateComponents(c.authCtx(ctx)).
		YumAsset(f).
		YumAssetFilename(filepath.Base(filePath))
	return c.doUpload(ctx, "rpm", repository, filePath, &req)
}

// UploadRaw uploads a single file to a Nexus raw (hosted) repository under
// directory, keeping the file's base name as the asset filename.
// See UploadDeb for why this bypasses the generic CLI path.
func (c *Client) UploadRaw(ctx context.Context, repository, directory, filePath string) error {
	f, err := openUploadFile("raw", filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	req := c.api.ComponentsAPI.CreateComponents(c.authCtx(ctx)).
		RawDirectory(directory).
		RawAsset1(f).
		RawAsset1Filename(filepath.Base(filePath))
	return c.doUpload(ctx, "raw", repository, filePath, &req)
}

// maven2AssetFileSetters map a 1-based asset slot (index 0 = asset1) to
// the SDK setter for that slot's file part. The SDK hard-codes three
// slots with no variadic form, so a small indexed table drives the loop
// in UploadMaven2Assets instead of duplicating it per slot.
var maven2AssetFileSetters = [MaxMaven2Assets]func(v3.ApiCreateComponentsRequest, *os.File) v3.ApiCreateComponentsRequest{
	func(r v3.ApiCreateComponentsRequest, f *os.File) v3.ApiCreateComponentsRequest {
		return r.Maven2Asset1(f)
	},
	func(r v3.ApiCreateComponentsRequest, f *os.File) v3.ApiCreateComponentsRequest {
		return r.Maven2Asset2(f)
	},
	func(r v3.ApiCreateComponentsRequest, f *os.File) v3.ApiCreateComponentsRequest {
		return r.Maven2Asset3(f)
	},
}

// maven2AssetExtensionSetters is maven2AssetFileSetters' counterpart for
// each slot's optional extension field.
var maven2AssetExtensionSetters = [MaxMaven2Assets]func(v3.ApiCreateComponentsRequest, string) v3.ApiCreateComponentsRequest{
	func(r v3.ApiCreateComponentsRequest, s string) v3.ApiCreateComponentsRequest {
		return r.Maven2Asset1Extension(s)
	},
	func(r v3.ApiCreateComponentsRequest, s string) v3.ApiCreateComponentsRequest {
		return r.Maven2Asset2Extension(s)
	},
	func(r v3.ApiCreateComponentsRequest, s string) v3.ApiCreateComponentsRequest {
		return r.Maven2Asset3Extension(s)
	},
}

// maven2AssetClassifierSetters is maven2AssetFileSetters' counterpart for
// each slot's optional classifier field.
var maven2AssetClassifierSetters = [MaxMaven2Assets]func(v3.ApiCreateComponentsRequest, string) v3.ApiCreateComponentsRequest{
	func(r v3.ApiCreateComponentsRequest, s string) v3.ApiCreateComponentsRequest {
		return r.Maven2Asset1Classifier(s)
	},
	func(r v3.ApiCreateComponentsRequest, s string) v3.ApiCreateComponentsRequest {
		return r.Maven2Asset2Classifier(s)
	},
	func(r v3.ApiCreateComponentsRequest, s string) v3.ApiCreateComponentsRequest {
		return r.Maven2Asset3Classifier(s)
	},
}

// UploadMaven2Assets uploads 1..MaxMaven2Assets sibling assets that share
// one set of coordinates (e.g. a jar, its pom, and a sources jar) to a
// Nexus maven2 (hosted) repository as a single createComponents request.
// See UploadDeb for why this bypasses the generic CLI path.
func (c *Client) UploadMaven2Assets(ctx context.Context, repository string, coords *Maven2Coordinates, assets []Maven2Asset) error {
	if len(assets) == 0 {
		return fmt.Errorf("nexus3: upload maven2: at least one asset is required")
	}
	if len(assets) > MaxMaven2Assets {
		return fmt.Errorf("nexus3: upload maven2: got %d assets, want at most %d", len(assets), MaxMaven2Assets)
	}

	files := make([]*os.File, 0, len(assets))
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()

	paths := make([]string, len(assets))
	for i, asset := range assets {
		paths[i] = asset.Path
		f, err := openUploadFile("maven2", asset.Path)
		if err != nil {
			return err
		}
		files = append(files, f)
	}

	req := c.api.ComponentsAPI.CreateComponents(c.authCtx(ctx)).
		Maven2GroupId(coords.GroupID).
		Maven2ArtifactId(coords.ArtifactID).
		Maven2Version(coords.Version).
		Maven2GeneratePom(coords.GeneratePOM)
	if coords.Packaging != "" {
		req = req.Maven2Packaging(coords.Packaging)
	}
	for i, asset := range assets {
		req = maven2AssetFileSetters[i](req, files[i])
		if asset.Extension != "" {
			req = maven2AssetExtensionSetters[i](req, asset.Extension)
		}
		if asset.Classifier != "" {
			req = maven2AssetClassifierSetters[i](req, asset.Classifier)
		}
	}

	return c.doUpload(ctx, "maven2", repository, strings.Join(paths, ", "), &req)
}

// UploadMaven2 uploads a single artifact to a Nexus maven2 (hosted)
// repository at the given coordinates. It is a one-asset convenience
// wrapper around UploadMaven2Assets.
func (c *Client) UploadMaven2(ctx context.Context, repository string, coords *Maven2Coordinates, asset Maven2Asset) error {
	return c.UploadMaven2Assets(ctx, repository, coords, []Maven2Asset{asset})
}

// SimpleFormats returns, sorted, the format names UploadSimple accepts:
// hosted formats whose upload needs nothing beyond the file itself.
func SimpleFormats() []string {
	return slices.Sorted(maps.Keys(simpleFormats))
}

// UploadSimple uploads a single file to a Nexus hosted repository whose
// format needs nothing beyond the file itself: go, helm, npm, nuget, pypi,
// or rubygems. Use UploadDeb, UploadRpm, UploadRaw, or UploadMaven2 for
// formats needing extra fields. See UploadDeb for why this bypasses the
// generic CLI path.
func (c *Client) UploadSimple(ctx context.Context, format, repository, filePath string) error {
	setAsset, ok := simpleFormats[format]
	if !ok {
		return fmt.Errorf("nexus3: upload: unsupported format %q, want one of %s "+
			"(or deb/rpm/raw/maven2 via their dedicated methods)", format, strings.Join(SimpleFormats(), ", "))
	}

	f, err := openUploadFile(format, filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	req := setAsset(c.api.ComponentsAPI.CreateComponents(c.authCtx(ctx)), f)
	return c.doUpload(ctx, format, repository, filePath, &req)
}
