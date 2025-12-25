package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/petroprotsakh/go-provider-mirror/internal/httpclient"
)

// Client is a provider registry client.
type Client struct {
	http *httpclient.Client
}

// NewClient creates a new registry client.
func NewClient() *Client {
	return &Client{
		http: httpclient.New(
			httpclient.Config{
				Timeout:    30 * time.Second,
				Retries:    3,
				MaxBackoff: 60 * time.Second,
			},
		),
	}
}

// ProviderVersions represents the response from the versions endpoint.
type ProviderVersions struct {
	Versions []ProviderVersion `json:"versions"`
}

// ProviderVersion represents a single provider version.
type ProviderVersion struct {
	Version   string             `json:"version"`
	Protocols []string           `json:"protocols"`
	Platforms []ProviderPlatform `json:"platforms"`
}

// ProviderPlatform represents a platform for a provider version.
type ProviderPlatform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// String returns the platform string (os_arch).
func (p ProviderPlatform) String() string {
	return fmt.Sprintf("%s_%s", p.OS, p.Arch)
}

// DownloadInfo represents the response from the download endpoint.
type DownloadInfo struct {
	Protocols           []string    `json:"protocols"`
	OS                  string      `json:"os"`
	Arch                string      `json:"arch"`
	Filename            string      `json:"filename"`
	DownloadURL         string      `json:"download_url"`
	SHA256Sum           string      `json:"shasum"`
	SHA256SumsURL       string      `json:"shasums_url"`
	SHA256SumsSignature string      `json:"shasums_signature_url"`
	SigningKeys         SigningKeys `json:"signing_keys"`
}

// SigningKeys represents GPG signing keys.
type SigningKeys struct {
	GPGPublicKeys []GPGPublicKey `json:"gpg_public_keys"`
}

// GPGPublicKey represents a GPG public key.
type GPGPublicKey struct {
	KeyID      string `json:"key_id"`
	ASCIIArmor string `json:"ascii_armor"`
}

// ServiceDiscovery represents the .well-known/terraform.json response.
type ServiceDiscovery struct {
	ProvidersV1 string `json:"providers.v1"`
}

// GetVersions retrieves all versions of a provider from a registry.
func (c *Client) GetVersions(
	ctx context.Context,
	hostname, namespace, name string,
) (*ProviderVersions, error) {
	baseURL, err := c.discoverService(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("service discovery failed: %w", err)
	}

	endpoint := fmt.Sprintf("%s%s/%s/versions", baseURL, namespace, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.http.Do(req, httpclient.WithRetry(), httpclient.WithAuth(hostname))
	if err != nil {
		return nil, fmt.Errorf("fetching versions: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
	}

	var versions ProviderVersions
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, fmt.Errorf("decoding versions: %w", err)
	}

	return &versions, nil
}

// GetDownloadInfo retrieves download information for a specific provider version and platform.
func (c *Client) GetDownloadInfo(
	ctx context.Context,
	hostname, namespace, name, version, os, arch string,
) (*DownloadInfo, error) {
	baseURL, err := c.discoverService(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("service discovery failed: %w", err)
	}

	endpoint := fmt.Sprintf(
		"%s%s/%s/%s/download/%s/%s",
		baseURL,
		namespace,
		name,
		version,
		os,
		arch,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.http.Do(req, httpclient.WithRetry(), httpclient.WithAuth(hostname))
	if err != nil {
		return nil, fmt.Errorf("fetching download info: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
	}

	var info DownloadInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding download info: %w", err)
	}

	return &info, nil
}

// discoverService performs service discovery for a registry hostname.
func (c *Client) discoverService(ctx context.Context, hostname string) (string, error) {
	discoveryURL := fmt.Sprintf("https://%s/.well-known/terraform.json", hostname)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating discovery request: %w", err)
	}

	// Service discovery doesn't need retry - we fall back to defaults on failure
	resp, err := c.http.Do(req)
	if err != nil {
		return c.defaultServiceURL(hostname)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return c.defaultServiceURL(hostname)
	}

	var discovery ServiceDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return "", fmt.Errorf("decoding discovery response: %w", err)
	}

	if discovery.ProvidersV1 == "" {
		return "", fmt.Errorf("no providers.v1 endpoint in discovery response")
	}

	// Handle relative URLs
	if strings.HasPrefix(discovery.ProvidersV1, "/") {
		return fmt.Sprintf("https://%s%s", hostname, discovery.ProvidersV1), nil
	}

	return discovery.ProvidersV1, nil
}

// defaultServiceURL returns the default provider API URL for well-known registries.
func (c *Client) defaultServiceURL(hostname string) (string, error) {
	switch hostname {
	case "registry.terraform.io":
		return "https://registry.terraform.io/v1/providers/", nil
	case "registry.opentofu.org":
		return "https://registry.opentofu.org/v1/providers/", nil
	default:
		return "", fmt.Errorf("unknown registry %s and service discovery failed", hostname)
	}
}

// ParsePlatform parses a platform string (os_arch) into OS and Arch.
func ParsePlatform(platform string) (os, arch string, err error) {
	// Handle URLs that might have been passed
	if u, err := url.Parse(platform); err == nil && u.Scheme != "" {
		platform = u.Path
	}

	parts := strings.Split(platform, "_")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid platform format: %s (expected os_arch)", platform)
	}

	return parts[0], parts[1], nil
}
