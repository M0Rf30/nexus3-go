package nexus3

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	v3 "github.com/sonatype-nexus-community/nexus-repo-api-client-go/v3"
)

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

// Maven2Coordinates identifies where UploadMaven2 places a single artifact.
// GroupID, ArtifactID, and Version are required by Nexus; Extension and
// Classifier are optional (Extension defaults server-side from the
// filename when empty); Packaging and GeneratePOM are optional.
type Maven2Coordinates struct {
	GroupID     string
	ArtifactID  string
	Version     string
	Extension   string
	Classifier  string
	Packaging   string
	GeneratePOM bool
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
		return fmt.Errorf("nexus3: upload %s %s to %s: %w", op, filePath, repository, err)
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

// UploadMaven2 uploads a single artifact to a Nexus maven2 (hosted)
// repository at the given coordinates. See UploadDeb for why this bypasses
// the generic CLI path.
func (c *Client) UploadMaven2(ctx context.Context, repository string, coords *Maven2Coordinates, filePath string) error {
	f, err := openUploadFile("maven2", filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	req := c.api.ComponentsAPI.CreateComponents(c.authCtx(ctx)).
		Maven2GroupId(coords.GroupID).
		Maven2ArtifactId(coords.ArtifactID).
		Maven2Version(coords.Version).
		Maven2Asset1(f).
		Maven2GeneratePom(coords.GeneratePOM)
	if coords.Extension != "" {
		req = req.Maven2Asset1Extension(coords.Extension)
	}
	if coords.Classifier != "" {
		req = req.Maven2Asset1Classifier(coords.Classifier)
	}
	if coords.Packaging != "" {
		req = req.Maven2Packaging(coords.Packaging)
	}
	return c.doUpload(ctx, "maven2", repository, filePath, &req)
}

// UploadSimple uploads a single file to a Nexus hosted repository whose
// format needs nothing beyond the file itself: go, helm, npm, nuget, pypi,
// or rubygems. Use UploadDeb, UploadRpm, UploadRaw, or UploadMaven2 for
// formats needing extra fields. See UploadDeb for why this bypasses the
// generic CLI path.
func (c *Client) UploadSimple(ctx context.Context, format, repository, filePath string) error {
	setAsset, ok := simpleFormats[format]
	if !ok {
		supported := make([]string, 0, len(simpleFormats))
		for f := range simpleFormats {
			supported = append(supported, f)
		}
		return fmt.Errorf("nexus3: upload: unsupported format %q, want one of %v "+
			"(or deb/rpm/raw/maven2 via their dedicated methods)", format, supported)
	}

	f, err := openUploadFile(format, filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	req := setAsset(c.api.ComponentsAPI.CreateComponents(c.authCtx(ctx)), f)
	return c.doUpload(ctx, format, repository, filePath, &req)
}
