package resolver

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/petroprotsakh/go-provider-mirror/internal/manifest"
)

// --- buildResolution tests ---

func TestBuildResolution_SingleProvider(t *testing.T) {
	versionsMap := map[versionKey]map[string]bool{
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {"linux_amd64": true, "darwin_arm64": true},
	}

	sourcesMap := map[versionKey]map[string]bool{
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {"hashicorp/null": true},
	}

	result := buildResolution(versionsMap, sourcesMap)

	if len(result.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(result.Providers))
	}

	p := result.Providers[0]
	if p.Source.Hostname != "registry.terraform.io" {
		t.Errorf("expected hostname registry.terraform.io, got %s", p.Source.Hostname)
	}

	if len(p.Versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(p.Versions))
	}

	v := p.Versions[0]
	if v.Version != "3.2.4" {
		t.Errorf("expected version 3.2.4, got %s", v.Version)
	}

	if len(v.Platforms) != 2 {
		t.Errorf("expected 2 platforms, got %d", len(v.Platforms))
	}

	if len(v.ManifestSources) != 1 || v.ManifestSources[0] != "hashicorp/null" {
		t.Errorf("expected manifest sources [hashicorp/null], got %v", v.ManifestSources)
	}
}

func TestBuildResolution_MultipleVersions(t *testing.T) {
	versionsMap := map[versionKey]map[string]bool{
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {"linux_amd64": true},
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.3",
		}: {"linux_amd64": true},
	}

	sourcesMap := map[versionKey]map[string]bool{
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {"hashicorp/null": true},
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.3",
		}: {"hashicorp/null": true},
	}

	result := buildResolution(versionsMap, sourcesMap)

	if len(result.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(result.Providers))
	}

	if len(result.Providers[0].Versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(result.Providers[0].Versions))
	}

	// Should be sorted descending
	if result.Providers[0].Versions[0].Version != "3.2.4" {
		t.Errorf("expected 3.2.4 first (descending), got %s", result.Providers[0].Versions[0].Version)
	}

	if result.Providers[0].Versions[1].Version != "3.2.3" {
		t.Errorf("expected 3.2.3 second, got %s", result.Providers[0].Versions[1].Version)
	}
}

func TestBuildResolution_MultipleProviders(t *testing.T) {
	versionsMap := map[versionKey]map[string]bool{
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "aws",
			version:   "5.0.0",
		}: {"linux_amd64": true},
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {"linux_amd64": true},
	}

	sourcesMap := map[versionKey]map[string]bool{
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "aws",
			version:   "5.0.0",
		}: {"hashicorp/aws": true},
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {"hashicorp/null": true},
	}

	result := buildResolution(versionsMap, sourcesMap)

	if len(result.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(result.Providers))
	}

	// Should be sorted by name
	names := []string{result.Providers[0].Source.Name, result.Providers[1].Source.Name}
	sort.Strings(names)

	if names[0] != "aws" || names[1] != "null" {
		t.Errorf("expected providers sorted alphabetically, got %v", names)
	}
}

func TestBuildResolution_MultipleRegistries(t *testing.T) {
	versionsMap := map[versionKey]map[string]bool{
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {"linux_amd64": true},
		{
			hostname:  "registry.opentofu.org",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {"linux_amd64": true},
	}

	sourcesMap := map[versionKey]map[string]bool{
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {"hashicorp/null": true},
		{
			hostname:  "registry.opentofu.org",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {"hashicorp/null": true},
	}

	result := buildResolution(versionsMap, sourcesMap)

	// Same namespace/name but different hostnames should be separate providers
	if len(result.Providers) != 2 {
		t.Fatalf("expected 2 providers (different registries), got %d", len(result.Providers))
	}

	hostnames := make(map[string]bool)
	for _, p := range result.Providers {
		hostnames[p.Source.Hostname] = true
	}

	if !hostnames["registry.terraform.io"] || !hostnames["registry.opentofu.org"] {
		t.Errorf("expected both registries in result, got %v", hostnames)
	}
}

func TestBuildResolution_MergesManifestSources(t *testing.T) {
	// Same version from multiple manifest sources
	versionsMap := map[versionKey]map[string]bool{
		{
			hostname:  "registry.opentofu.org",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {"linux_amd64": true, "darwin_arm64": true},
	}

	sourcesMap := map[versionKey]map[string]bool{
		{
			hostname:  "registry.opentofu.org",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {
			"hashicorp/null":                       true,
			"registry.opentofu.org/hashicorp/null": true,
		},
	}

	result := buildResolution(versionsMap, sourcesMap)

	if len(result.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(result.Providers))
	}

	sources := result.Providers[0].Versions[0].ManifestSources
	if len(sources) != 2 {
		t.Fatalf("expected 2 manifest sources, got %d", len(sources))
	}

	// Should be sorted alphabetically
	if sources[0] != "hashicorp/null" {
		t.Errorf("expected 'hashicorp/null' first (alphabetically), got %s", sources[0])
	}
}

func TestBuildResolution_PlatformsSorted(t *testing.T) {
	versionsMap := map[versionKey]map[string]bool{
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {
			"windows_amd64": true,
			"linux_amd64":   true,
			"darwin_arm64":  true,
		},
	}

	sourcesMap := map[versionKey]map[string]bool{
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {"hashicorp/null": true},
	}

	result := buildResolution(versionsMap, sourcesMap)

	platforms := result.Providers[0].Versions[0].Platforms

	expected := []string{"darwin_arm64", "linux_amd64", "windows_amd64"}
	if !reflect.DeepEqual(platforms, expected) {
		t.Errorf("expected platforms %v (sorted), got %v", expected, platforms)
	}
}

func TestBuildResolution_VersionsSortedDescending(t *testing.T) {
	versionsMap := map[versionKey]map[string]bool{
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.1.0",
		}: {"linux_amd64": true},
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.4",
		}: {"linux_amd64": true},
		{
			hostname:  "registry.terraform.io",
			namespace: "hashicorp",
			name:      "null",
			version:   "3.2.0",
		}: {"linux_amd64": true},
	}

	sourcesMap := map[versionKey]map[string]bool{}
	for k := range versionsMap {
		sourcesMap[k] = map[string]bool{"hashicorp/null": true}
	}

	result := buildResolution(versionsMap, sourcesMap)

	versions := result.Providers[0].Versions
	if versions[0].Version != "3.2.4" {
		t.Errorf("expected 3.2.4 first, got %s", versions[0].Version)
	}
	if versions[1].Version != "3.2.0" {
		t.Errorf("expected 3.2.0 second, got %s", versions[1].Version)
	}
	if versions[2].Version != "3.1.0" {
		t.Errorf("expected 3.1.0 third, got %s", versions[2].Version)
	}
}

func TestBuildResolution_Empty(t *testing.T) {
	result := buildResolution(
		map[versionKey]map[string]bool{},
		map[versionKey]map[string]bool{},
	)

	if len(result.Providers) != 0 {
		t.Errorf("expected 0 providers for empty input, got %d", len(result.Providers))
	}
}

// --- ResolvedProvider tests ---

func TestResolvedProvider_Source(t *testing.T) {
	rp := ResolvedProvider{
		Source: manifest.ProviderSource{
			Hostname:  "registry.terraform.io",
			Namespace: "hashicorp",
			Name:      "aws",
		},
		Versions: []ResolvedVersion{
			{Version: "5.0.0", Platforms: []string{"linux_amd64"}},
		},
	}

	if rp.Source.String() != "registry.terraform.io/hashicorp/aws" {
		t.Errorf("unexpected source string: %s", rp.Source.String())
	}
}

// --- ResolvedVersion tests ---

func TestResolvedVersion_Fields(t *testing.T) {
	rv := ResolvedVersion{
		Version:         "3.2.4",
		Platforms:       []string{"linux_amd64", "darwin_arm64"},
		ManifestSources: []string{"hashicorp/null"},
	}

	if rv.Version != "3.2.4" {
		t.Errorf("unexpected version: %s", rv.Version)
	}

	if len(rv.Platforms) != 2 {
		t.Errorf("expected 2 platforms, got %d", len(rv.Platforms))
	}

	if len(rv.ManifestSources) != 1 {
		t.Errorf("expected 1 manifest source, got %d", len(rv.ManifestSources))
	}
}

// --- Resolution tests ---

func TestResolution_Empty(t *testing.T) {
	r := &Resolution{}

	if len(r.Providers) != 0 {
		t.Errorf("expected empty providers, got %d", len(r.Providers))
	}
}

// --- versionKey tests ---

func TestVersionKey_Uniqueness(t *testing.T) {
	key1 := versionKey{
		hostname:  "registry.terraform.io",
		namespace: "hashicorp",
		name:      "null",
		version:   "3.2.4",
	}

	key2 := versionKey{
		hostname:  "registry.terraform.io",
		namespace: "hashicorp",
		name:      "null",
		version:   "3.2.4",
	}

	key3 := versionKey{
		hostname:  "registry.opentofu.org", // different
		namespace: "hashicorp",
		name:      "null",
		version:   "3.2.4",
	}

	// Same values should be equal
	if key1 != key2 {
		t.Error("identical keys should be equal")
	}

	// Different hostname should not be equal
	if key1 == key3 {
		t.Error("keys with different hostnames should not be equal")
	}

	// Test as map key
	m := make(map[versionKey]bool)
	m[key1] = true
	m[key2] = true // should overwrite
	m[key3] = true

	if len(m) != 2 {
		t.Errorf("expected 2 unique keys, got %d", len(m))
	}
}

// --- New tests ---

func TestNew(t *testing.T) {
	r := New(nil)

	if r == nil {
		t.Fatal("New() should return non-nil resolver")
	}

	if r.client != nil {
		t.Error("client should be nil when passed nil")
	}
}

// --- Context cancellation tests ---

func TestResolve_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	r := New(nil)

	m := &manifest.Manifest{
		Defaults: manifest.Defaults{
			Engines:   []manifest.Engine{manifest.EngineTerraform},
			Platforms: []string{"linux_amd64"},
		},
		Providers: []manifest.Provider{
			{Source: "hashicorp/null", Versions: []string{"3.2.4"}},
		},
	}

	_, err := r.Resolve(ctx, m)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
