# provider-mirror

[![Build](https://github.com/petroprotsakh/go-provider-mirror/actions/workflows/test.yml/badge.svg)](https://github.com/petroprotsakh/go-provider-mirror/actions/workflows/test.yml)

A CLI tool that builds Terraform and OpenTofu provider mirrors from a declarative YAML manifest.

The tool focuses on **manifest-driven, reproducible mirrors**, rather than scanning existing `.tf` configurations.

## Why

- **Air-gapped environments** — Pre-download providers for networks without internet access
- **Faster CI** — Local mirror is faster than registry lookups
- **Declarative version selection** — Explicitly define which provider versions and platforms are allowed
- **Terraform & OpenTofu support** — Build a single mirror usable by both engines

## Usage

```shell
$ provider-mirror build --manifest ./examples/mirror.yaml -o mirror
Building mirror from ./examples/mirror.yaml
Output directory: mirror
Providers: 3

→ Resolving provider versions...
  Resolved 5 provider(s), 5 version(s) in 420ms
  Total downloads: 8

→ Downloading providers (8 files)...
Total 8 / 8   100 %
  Downloaded: 8, Cache hits: 0, Total: 8 in 11.402s

→ Writing mirror...
  Wrote mirror in 2.999s

Mirror contents:
  registry.opentofu.org/hashicorp/aws
    5.100.0 (2 platforms)
  registry.opentofu.org/hashicorp/null
    2.1.2 (1 platforms)
  registry.terraform.io/hashicorp/aws
    5.100.0 (2 platforms)
  registry.terraform.io/hashicorp/null
    2.1.2 (1 platforms)
  registry.terraform.io/hashicorp/random
    3.6.0 (2 platforms)

✓ Mirror built successfully
```

## Install

```bash
go install github.com/petroprotsakh/go-provider-mirror/cmd/provider-mirror@latest
```

Or download a prebuilt binary from the
[Releases](https://github.com/petroprotsakh/go-provider-mirror/releases) page.

## Quick Start

1. Create a manifest file `mirror.yaml`:

```yaml
defaults:
  engines:
    - terraform
  platforms:
    - linux_amd64
    - darwin_arm64

providers:
  - source: hashicorp/aws
    versions: ["~> 5.0"]
  - source: hashicorp/null
    versions: ["3.2.4"]
```

2. Build the mirror:

```bash
provider-mirror build --manifest mirror.yaml --output ./mirror
```

3. Configure Terraform or OpenTofu to use it:

```hcl
# ~/.terraformrc or ~/.tofurc
provider_installation {
  filesystem_mirror {
    path = "/path/to/mirror"
  }
}
```

## Commands

```bash
# Preview what would be downloaded
provider-mirror plan --manifest mirror.yaml

# Build the mirror
provider-mirror build --manifest mirror.yaml --output ./mirror

# Verify mirror integrity
provider-mirror verify --mirror ./mirror
```

## Manifest Format

```yaml
defaults:
  engines:          # terraform, opentofu, or both
    - terraform
    - opentofu
  platforms:        # os_arch format
    - linux_amd64
    - darwin_arm64

providers:
  - source: hashicorp/aws           # namespace/name
    versions: ["~> 5.0", "~> 4.0"]  # version constraints

  - source: hashicorp/null
    versions: ["3.2.4"]
    platforms:                     # override defaults
      - linux_amd64
```

Version constraints follow [Terraform's syntax](https://developer.hashicorp.com/terraform/language/expressions/version-constraints): `=`, `!=`, `>`, `>=`, `<`, `<=`, `~>`.

See [examples](examples/) for more.

## Private Registries

For private registries, set credentials via environment variables:

```bash
# Format: PM_TOKEN_<hostname_with_underscores>
export PM_TOKEN_registry_example_com="your-token"
```

The tool also reads `TF_TOKEN_*` variables for Terraform CLI compatibility.

## Output

The generated mirror follows Terraform’s filesystem mirror layout and includes
a `mirror.lock` file with checksums and metadata to make builds reproducible:

```
mirror/
├── mirror.lock
└── registry.terraform.io/
    └── hashicorp/
        └── aws/
            ├── index.json
            ├── 5.0.0.json
            └── terraform-provider-aws_5.0.0_linux_amd64.zip
```

## Scope and Non-Goals

- This tool does **not** scan `.tf` files or Terraform state
- It does **not** replace Terraform/OpenTofu commands
- It does **not** invoke or depend on Terraform or OpenTofu binaries
- All inputs come from the manifest file

## License

Apache-2.0
