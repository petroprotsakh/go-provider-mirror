# provider-mirror

A CLI tool that builds Terraform and OpenTofu provider mirrors from a YAML manifest.

## Why

- **Air-gapped environments** — Pre-download providers for networks without internet access
- **Faster CI** — Local mirror is faster than registry lookups
- **Version control** — Declare exactly which provider versions you need

## Install

```bash
go install github.com/petroprotsakh/go-provider-mirror/cmd/provider-mirror@latest
```

Or download from [Releases](https://github.com/petroprotsakh/go-provider-mirror/releases).

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

3. Configure Terraform to use it:

```hcl
# ~/.terraformrc
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
    platforms:                       # override defaults
      - linux_amd64
```

## Options

```
--manifest, -m    Path to manifest file (required)
--output, -o      Output directory (default: ./mirror)
--cache           Cache directory (default: system temp)
--no-cache        Skip cache, re-download everything
--concurrency     Parallel downloads (default: 8)
--retries         Retry failed downloads (default: 3)
-v                Verbose output
-vv               Debug output
-q                Quiet, errors only
```

## Output

The mirror follows Terraform's [filesystem mirror](https://developer.hashicorp.com/terraform/cli/config/config-file#filesystem_mirror) layout:

```
mirror/
├── mirror.lock                              # checksums and metadata
└── registry.terraform.io/
    └── hashicorp/
        └── aws/
            ├── index.json
            ├── 5.0.0.json
            └── terraform-provider-aws_5.0.0_linux_amd64.zip
```

## License

MIT

