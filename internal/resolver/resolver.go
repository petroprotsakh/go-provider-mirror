package resolver

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/go-version"

	"github.com/petroprotsakh/go-provider-mirror/internal/manifest"
	"github.com/petroprotsakh/go-provider-mirror/internal/registry"
)

// Resolver resolves provider version constraints against registries
type Resolver struct {
	client *registry.Client
}

// New creates a new resolver
func New(client *registry.Client) *Resolver {
	return &Resolver{
		client: client,
	}
}

// ResolvedProvider represents a provider with resolved concrete versions
type ResolvedProvider struct {
	Source   manifest.ProviderSource
	Versions []ResolvedVersion
}

// ResolvedVersion represents a single resolved version with platforms
type ResolvedVersion struct {
	Version         string
	Platforms       []string // os_arch format
	ManifestSources []string // original source specs from manifest that contributed to this version
}

// Resolution represents the complete resolution result
type Resolution struct {
	Providers []ResolvedProvider
}

// Resolve resolves all providers from the manifest to concrete versions.
// Each version constraint in the manifest is resolved independently to its
// latest matching version. Multiple provider blocks for the same provider
// are merged, and the result is deduplicated.
func (r *Resolver) Resolve(ctx context.Context, m *manifest.Manifest) (*Resolution, error) {
	// Check for cancellation upfront
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	expanded, err := m.GetExpandedProviders()
	if err != nil {
		return nil, fmt.Errorf("expanding providers: %w", err)
	}

	// For each expanded provider, resolve each version constraint independently
	// Key: hostname/namespace/name/version -> platforms
	versionsMap := make(map[versionKey]map[string]bool) // key -> set of platforms
	sourcesMap := make(map[versionKey]map[string]bool)  // key -> set of manifest sources

	// Group expansions by provider identity and constraint for resolution
	// Key: namespace/name + constraint string
	type constraintGroup struct {
		constraint string
		expansions []manifest.ExpandedProvider
	}
	constraintGroups := make(map[string][]constraintGroup)

	// First pass: group expansions by provider identity and constraint
	for _, ep := range expanded {
		providerKey := fmt.Sprintf("%s/%s", ep.Source.Namespace, ep.Source.Name)

		for _, constraintStr := range ep.Versions {
			// Find or create group for this constraint
			found := false
			for i, cg := range constraintGroups[providerKey] {
				if cg.constraint == constraintStr {
					constraintGroups[providerKey][i].expansions = append(
						constraintGroups[providerKey][i].expansions,
						manifest.ExpandedProvider{
							Source:     ep.Source,
							Versions:   []string{constraintStr},
							Platforms:  ep.Platforms,
							Engine:     ep.Engine,
							SourceSpec: ep.SourceSpec,
						},
					)
					found = true
					break
				}
			}
			if !found {
				constraintGroups[providerKey] = append(
					constraintGroups[providerKey], constraintGroup{
						constraint: constraintStr,
						expansions: []manifest.ExpandedProvider{
							{
								Source:     ep.Source,
								Versions:   []string{constraintStr},
								Platforms:  ep.Platforms,
								Engine:     ep.Engine,
								SourceSpec: ep.SourceSpec,
							},
						},
					},
				)
			}
		}
	}

	// Second pass: resolve each constraint group
	for _, groups := range constraintGroups {
		for _, cg := range groups {
			// Check for cancellation
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			resolvedVersion, err := r.resolveConstraintGroup(ctx, cg.constraint, cg.expansions)
			if err != nil {
				return nil, err
			}

			// Add to results
			for _, rv := range resolvedVersion {
				key := versionKey{
					hostname:  rv.Provider.Hostname,
					namespace: rv.Provider.Namespace,
					name:      rv.Provider.Name,
					version:   rv.Version,
				}

				if versionsMap[key] == nil {
					versionsMap[key] = make(map[string]bool)
				}
				for _, p := range rv.Platforms {
					versionsMap[key][p] = true
				}

				// Track which manifest sources contributed to this version
				if sourcesMap[key] == nil {
					sourcesMap[key] = make(map[string]bool)
				}
				sourcesMap[key][rv.ManifestSource] = true
			}
		}
	}

	// Build final result
	return buildResolution(versionsMap, sourcesMap), nil
}

// resolvedVersionResult holds the result for a single version resolution
type resolvedVersionResult struct {
	Provider       manifest.ProviderSource
	Version        string
	Platforms      []string
	ManifestSource string // original source spec from manifest (e.g., "hashicorp/null")
}

// resolveConstraintGroup resolves a single constraint across multiple registry expansions.
// Each registry resolves independently to its own latest matching version.
// This allows registries to have different available versions without failing.
func (r *Resolver) resolveConstraintGroup(
	ctx context.Context,
	constraintStr string,
	expansions []manifest.ExpandedProvider,
) ([]resolvedVersionResult, error) {
	if len(expansions) == 0 {
		return nil, nil
	}

	constraint, err := version.NewConstraint(constraintStr)
	if err != nil {
		return nil, fmt.Errorf("parsing constraint %q: %w", constraintStr, err)
	}

	var results []resolvedVersionResult

	for _, ep := range expansions {
		// Fetch available versions from registry
		pvs, err := r.client.GetVersions(
			ctx,
			ep.Source.Hostname,
			ep.Source.Namespace,
			ep.Source.Name,
		)
		if err != nil {
			return nil, fmt.Errorf("fetching versions for %s: %w", ep.Source.String(), err)
		}

		// Find all matching versions
		var matchingVersions []*version.Version
		versionToPlatforms := make(map[string][]registry.ProviderPlatform)

		for _, pv := range pvs.Versions {
			v, err := version.NewVersion(pv.Version)
			if err != nil {
				continue
			}
			if constraint.Check(v) {
				matchingVersions = append(matchingVersions, v)
				versionToPlatforms[pv.Version] = pv.Platforms
			}
		}

		if len(matchingVersions) == 0 {
			return nil, fmt.Errorf(
				"no versions of %s match constraint %q",
				ep.Source.String(), constraintStr,
			)
		}

		// Sort descending (newest first)
		sort.Slice(
			matchingVersions, func(i, j int) bool {
				return matchingVersions[i].GreaterThan(matchingVersions[j])
			},
		)

		// Select latest matching version for THIS registry
		selectedVersion := matchingVersions[0].Original()

		// Check platform availability for selected version
		availablePlatforms := make(map[string]bool)
		for _, p := range versionToPlatforms[selectedVersion] {
			availablePlatforms[p.String()] = true
		}

		var platforms []string
		for _, requested := range ep.Platforms {
			if availablePlatforms[requested] {
				platforms = append(platforms, requested)
			} else {
				return nil, fmt.Errorf(
					"provider %s version %s does not have platform %s",
					ep.Source.String(), selectedVersion, requested,
				)
			}
		}

		results = append(
			results, resolvedVersionResult{
				Provider:       ep.Source,
				Version:        selectedVersion,
				Platforms:      platforms,
				ManifestSource: ep.SourceSpec,
			},
		)
	}

	return results, nil
}

// versionKey identifies a unique provider version (artifact identity).
type versionKey struct {
	hostname  string
	namespace string
	name      string
	version   string
}

// versionData holds platforms and manifest sources for a version
type versionData struct {
	platforms []string
	sources   []string
}

// buildResolution converts the map-based results into the Resolution structure
func buildResolution(
	versionsMap map[versionKey]map[string]bool,
	sourcesMap map[versionKey]map[string]bool,
) *Resolution {
	// Group by provider (hostname/namespace/name)
	type providerKey struct {
		hostname  string
		namespace string
		name      string
	}

	grouped := make(map[providerKey]map[string]*versionData) // key -> version -> data

	for vk, platforms := range versionsMap {
		pk := providerKey{
			hostname:  vk.hostname,
			namespace: vk.namespace,
			name:      vk.name,
		}

		if grouped[pk] == nil {
			grouped[pk] = make(map[string]*versionData)
		}

		if grouped[pk][vk.version] == nil {
			grouped[pk][vk.version] = &versionData{}
		}

		// Collect platforms
		platformSet := make(map[string]bool)
		for _, p := range grouped[pk][vk.version].platforms {
			platformSet[p] = true
		}
		for p := range platforms {
			platformSet[p] = true
		}
		var platformList []string
		for p := range platformSet {
			platformList = append(platformList, p)
		}
		sort.Strings(platformList)
		grouped[pk][vk.version].platforms = platformList

		// Collect manifest sources
		sourceSet := make(map[string]bool)
		for _, s := range grouped[pk][vk.version].sources {
			sourceSet[s] = true
		}
		for s := range sourcesMap[vk] {
			sourceSet[s] = true
		}
		var sourceList []string
		for s := range sourceSet {
			sourceList = append(sourceList, s)
		}
		sort.Strings(sourceList)
		grouped[pk][vk.version].sources = sourceList
	}

	// Build Resolution
	result := &Resolution{}

	// Sort provider keys for deterministic output
	var providerKeys []providerKey
	for pk := range grouped {
		providerKeys = append(providerKeys, pk)
	}
	sort.Slice(
		providerKeys, func(i, j int) bool {
			if providerKeys[i].hostname != providerKeys[j].hostname {
				return providerKeys[i].hostname < providerKeys[j].hostname
			}
			if providerKeys[i].namespace != providerKeys[j].namespace {
				return providerKeys[i].namespace < providerKeys[j].namespace
			}
			return providerKeys[i].name < providerKeys[j].name
		},
	)

	for _, pk := range providerKeys {
		versions := grouped[pk]

		// Sort versions descending
		var versionStrs []string
		for v := range versions {
			versionStrs = append(versionStrs, v)
		}
		sort.Slice(
			versionStrs, func(i, j int) bool {
				vi, _ := version.NewVersion(versionStrs[i])
				vj, _ := version.NewVersion(versionStrs[j])
				if vi != nil && vj != nil {
					return vi.GreaterThan(vj)
				}
				return versionStrs[i] > versionStrs[j]
			},
		)

		var resolvedVersions []ResolvedVersion
		for _, v := range versionStrs {
			data := versions[v]
			resolvedVersions = append(
				resolvedVersions, ResolvedVersion{
					Version:         v,
					Platforms:       data.platforms,
					ManifestSources: data.sources,
				},
			)
		}

		result.Providers = append(
			result.Providers, ResolvedProvider{
				Source: manifest.ProviderSource{
					Hostname:  pk.hostname,
					Namespace: pk.namespace,
					Name:      pk.name,
				},
				Versions: resolvedVersions,
			},
		)
	}

	return result
}
