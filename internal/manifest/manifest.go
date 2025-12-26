package manifest

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Engine represents a supported IaC engine
type Engine string

const (
	EngineTerraform Engine = "terraform"
	EngineOpenTofu  Engine = "opentofu"
)

// IsValid returns true if the engine is a supported value
func (e Engine) IsValid() bool {
	switch e {
	case EngineTerraform, EngineOpenTofu:
		return true
	default:
		return false
	}
}

// DefaultRegistry returns the default registry for an engine
func (e Engine) DefaultRegistry() string {
	switch e {
	case EngineTerraform:
		return "registry.terraform.io"
	case EngineOpenTofu:
		return "registry.opentofu.org"
	default:
		return ""
	}
}

// Manifest represents the complete mirror manifest
type Manifest struct {
	Defaults  Defaults   `yaml:"defaults"`
	Providers []Provider `yaml:"providers"`
}

// Defaults contains default settings applied to all providers
type Defaults struct {
	Engines   []Engine `yaml:"engines"`
	Platforms []string `yaml:"platforms"`
}

// Provider represents a single provider entry in the manifest
type Provider struct {
	Source    string   `yaml:"source"`
	Versions  []string `yaml:"versions"`
	Engines   []Engine `yaml:"engines,omitempty"`   // overrides defaults
	Platforms []string `yaml:"platforms,omitempty"` // overrides defaults
}

// ProviderSource represents a parsed provider address
type ProviderSource struct {
	Hostname  string
	Namespace string
	Name      string
}

// String returns the full provider address
func (p ProviderSource) String() string {
	return fmt.Sprintf("%s/%s/%s", p.Hostname, p.Namespace, p.Name)
}

// Load reads and parses the manifest file
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	return Parse(data)
}

// Parse parses manifest YAML data
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	if err := m.Validate(); err != nil {
		return nil, err
	}

	m.applyDefaults()

	return &m, nil
}

// Validate checks that the manifest is well-formed
func (m *Manifest) Validate() error {
	for _, e := range m.Defaults.Engines {
		if !e.IsValid() {
			return fmt.Errorf("unsupported engine: %s", e)
		}
	}

	if len(m.Providers) == 0 {
		return fmt.Errorf("manifest must specify at least one provider")
	}

	for i, p := range m.Providers {
		if p.Source == "" {
			return fmt.Errorf("provider %d: source is required", i)
		}
		if len(p.Versions) == 0 {
			return fmt.Errorf("provider %s: at least one version constraint is required", p.Source)
		}
		for _, e := range p.Engines {
			if !e.IsValid() {
				return fmt.Errorf("provider %s: unsupported engine: %s", p.Source, e)
			}
		}
		if len(p.Engines) == 0 && len(m.Defaults.Engines) == 0 {
			return fmt.Errorf(
				"provider %s: no engines specified (set defaults.engines or provider-level engines)",
				p.Source,
			)
		}
	}

	return nil
}

// applyDefaults fills in default values where not specified
func (m *Manifest) applyDefaults() {
	for i := range m.Providers {
		if len(m.Providers[i].Engines) == 0 {
			m.Providers[i].Engines = m.Defaults.Engines
		}
		if len(m.Providers[i].Platforms) == 0 {
			m.Providers[i].Platforms = m.Defaults.Platforms
		}
	}
}

// ParseProviderSource parses a provider source string into its components
func ParseProviderSource(source string) (ProviderSource, error) {
	parts := strings.Split(source, "/")

	switch len(parts) {
	case 2:
		// namespace/name
		return ProviderSource{
			Namespace: parts[0],
			Name:      parts[1],
		}, nil
	case 3:
		// hostname/namespace/name
		return ProviderSource{
			Hostname:  parts[0],
			Namespace: parts[1],
			Name:      parts[2],
		}, nil
	default:
		return ProviderSource{}, fmt.Errorf(
			"invalid provider source format: %s (expected namespace/name or hostname/namespace/name)",
			source,
		)
	}
}

// expandProvider expands a provider specification across configured engines
func (m *Manifest) expandProvider(p Provider) ([]ExpandedProvider, error) {
	parsed, err := ParseProviderSource(p.Source)
	if err != nil {
		return nil, err
	}

	var result []ExpandedProvider

	if parsed.Hostname != "" {
		// Explicit hostname
		result = append(
			result, ExpandedProvider{
				Source:     parsed,
				Versions:   p.Versions,
				Platforms:  p.Platforms,
				SourceSpec: p.Source,
			},
		)
	} else {
		// No hostname - expand per engine
		for _, engine := range p.Engines {
			hostname := engine.DefaultRegistry()
			expanded := parsed
			expanded.Hostname = hostname

			result = append(
				result, ExpandedProvider{
					Source:     expanded,
					Versions:   p.Versions,
					Platforms:  p.Platforms,
					Engine:     engine,
					SourceSpec: p.Source,
				},
			)
		}
	}

	return result, nil
}

// ExpandedProvider represents a provider with a fully resolved source
type ExpandedProvider struct {
	Source     ProviderSource
	Versions   []string // constraints
	Platforms  []string
	Engine     Engine // empty if explicit hostname
	SourceSpec string // original source specification
}

// GetExpandedProviders returns all providers expanded across engines
func (m *Manifest) GetExpandedProviders() ([]ExpandedProvider, error) {
	var all []ExpandedProvider
	for _, p := range m.Providers {
		expanded, err := m.expandProvider(p)
		if err != nil {
			return nil, fmt.Errorf("expanding provider %s: %w", p.Source, err)
		}
		all = append(all, expanded...)
	}
	return all, nil
}
