package planner

import (
	"context"
	"fmt"

	"github.com/petroprotsakh/go-provider-mirror/internal/manifest"
	"github.com/petroprotsakh/go-provider-mirror/internal/registry"
	"github.com/petroprotsakh/go-provider-mirror/internal/resolver"
)

// Planner plans a mirror build without downloading
type Planner struct {
	manifest *manifest.Manifest
	client   *registry.Client
}

// New creates a new planner
func New(manifestPath string) (*Planner, error) {
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}

	return &Planner{
		manifest: m,
		client:   registry.NewClient(nil), // use defaults
	}, nil
}

// Plan represents a build plan
type Plan struct {
	Providers      []PlannedProvider
	TotalVersions  int
	TotalDownloads int
}

// PlannedProvider represents a provider in the plan
type PlannedProvider struct {
	Source   string
	Hostname string
	Versions []PlannedVersion
}

// PlannedVersion represents a version in the plan
type PlannedVersion struct {
	Version   string
	Platforms []string
}

// Plan creates a build plan
func (p *Planner) Plan(ctx context.Context) (*Plan, error) {
	res := resolver.New(p.client)
	resolution, err := res.Resolve(ctx, p.manifest)
	if err != nil {
		return nil, fmt.Errorf("resolving versions: %w", err)
	}

	plan := &Plan{}

	for _, rp := range resolution.Providers {
		pp := PlannedProvider{
			Source:   rp.Source.String(),
			Hostname: rp.Source.Hostname,
		}

		for _, rv := range rp.Versions {
			pv := PlannedVersion{
				Version:   rv.Version,
				Platforms: rv.Platforms,
			}
			pp.Versions = append(pp.Versions, pv)
			plan.TotalVersions++
			plan.TotalDownloads += len(rv.Platforms)
		}

		plan.Providers = append(plan.Providers, pp)
	}

	return plan, nil
}
