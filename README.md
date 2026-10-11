# xema-terraform-provider

A [Terraform](https://www.terraform.io) provider for the [Xema](https://xema.dev)
platform — Xema-as-Code. It binds to the Xema **control-plane-api** and lets you
declare Xema platform resources as Terraform configuration, with Terraform owning
state and drift detection.

## Provider configuration

The provider talks to a single control-plane endpoint with a bearer token. With
`org` set it acts inside that one org (org-admin token). Without `org` it runs in
**operator mode** (platform-admin token). Each attribute has an environment-variable
fallback.

| Attribute  | Env var          | Description                                   |
| ---------- | ---------------- | --------------------------------------------- |
| `endpoint` | `XEMA_ENDPOINT`  | Control-plane-api base URL.                   |
| `org`      | `XEMA_ORG`       | Xema organization id (optional; omit for operator mode). |
| `token`    | `XEMA_TOKEN`     | Bearer token (sensitive): org-admin, or platform-admin in operator mode. |

```hcl
terraform {
  required_providers {
    xema = {
      source = "xema-dev/xema"
    }
  }
}

provider "xema" {
  endpoint = "https://control-plane.xema.dev"
  org      = "org_123"
  token    = var.xema_token
}
```

### Operator mode and the two-provider pattern

Operator mode needs a realm `platform_admin` user token (a user JWT, not a service token); the control plane relays it to identity-api, which authorizes the acting human. No `X-Xema-Org-Id` header is required or sent.

Only `xema_org` is installation-scoped. Every other resource and data source,
including `data.xema_org`, is org-scoped. Without `org` the provider never falls
back to a default org: any org-scoped resource or data source fails at plan with
"this resource is org-scoped; configure the provider with org".

To create the first org and then manage what lives in it, use two providers: an
aliased operator provider with no `org`, and an org-scoped provider whose `org`
is the created org's id.

```hcl
provider "xema" {
  alias    = "operator"
  endpoint = "https://control-plane.xema.dev"
  token    = var.platform_admin_token
}

resource "xema_org" "acme" {
  provider     = xema.operator
  name         = "acme"
  display_name = "Acme"
}

provider "xema" {
  endpoint = "https://control-plane.xema.dev"
  org      = xema_org.acme.id
  token    = var.org_admin_token
}

resource "xema_project" "demo" {
  name = "demo"
}
```

A single `terraform apply` from empty state works: Terraform accepts the unknown
`org` at plan, plans the org-scoped resources against it, and configures the
second provider with the real id once `xema_org.acme` exists (verified with
Terraform against a mock control plane, create and destroy). The org-scoped
provider holds no usable client until then, so nothing can reach the control
plane with a guessed org. Data sources read through a provider whose `org` is still
unknown at plan were not exercised; if you hit a problem there, or prefer separate blast radii, split
into two stages (one state creates the org, a second takes `org` from its output).

## Resources

Each resource kind below has a matching data source (`data.xema_<kind>`) that reads by `id`.

| Terraform                       | Kind                    | Backing service       |
| ------------------------------- | ----------------------- | --------------------- |
| `xema_project`                  | `project`               | project-registry-api  |
| `xema_provider`                 | `provider`              | llm-registry-api      |
| `xema_model`                    | `model`                 | llm-registry-api      |
| `xema_model_strategy`           | `model-strategy`        | llm-registry-api      |
| `xema_model_resolution_rule`    | `model-resolution-rule` | llm-registry-api      |
| `xema_agent`                    | `agent`                 | llm-registry-api      |
| `xema_skill`                    | `skill`                 | skill-registry-api    |
| `xema_role`                     | `role`                  | authorization-api     |
| `xema_grant`                    | `grant`                 | authorization-api     |
| `xema_team`                     | `team`                  | authorization-api     |
| `xema_environment`              | `environment`           | authorization-api     |
| `xema_space`                    | `space`                 | space-registry-api    |
| `xema_org`                      | `org`                   | identity-api          |
| `xema_deliverable_spec`         | `deliverable-spec`      | deliverable-specs-api |
| `xema_biome_install`            | `biome-install`         | biome-host-api        |
| `xema_portal`                   | `portal`                | app-runtime-api       |

Opaque object/array fields (e.g. `metadata`, `config_json`, `branding`,
`lockfile`, `integrations`, `resources`, `access_grants`) are typed as
normalized JSON strings — Terraform compares them semantically, so formatting
or key-order differences never show as drift.

> **`xema_org`** (resource) is installation-scoped: creating, updating and
> deleting an org uses the control-plane installation routes, needs a
> platform-admin token, and works in operator mode (no `org`). Installation authority
> never implies org admin, and vice versa. An org admin reads their own org via
> the `xema_org` data source.
> **`xema_biome_install`** manages **org-scoped** installs (`projectId` null);
> the pinned biome version is service-managed and is not a declarable field.
> **`xema_space`** updates `classification` in place; changing `ref`,
> `display_name`, or `labels` forces replacement (the owning service's update is
> classification-only).

> **Note on `xema_provider.api_key`:** the API key is write-only — the control
> plane never returns it on read, so it is not drift-detected. Secrets never
> round-trip through the control plane.

Every kind also has a matching **data source** (`data.xema_<kind>`) for
read-by-id lookups: the `id` is the only input and every spec field is exposed
as a computed attribute.

### Distribution locks are not a provider concern

The provider publishes no distribution lock.
A lock is the sealed artifact `xema edition seal` produces.

## Installing from a release

Tagging `vX.Y.Z` runs `.github/workflows/release.yml` (goreleaser) and attaches
to the GitHub release: `terraform-provider-xema_<ver>_<os>_<arch>.zip` for
linux and darwin on amd64 and arm64, `terraform-provider-xema_<ver>_SHA256SUMS`,
and `terraform-provider-xema_<ver>_manifest.json`.

**Not signed.** There is no public Terraform Registry listing and no GPG key,
so goreleaser's usual GPG signature of `SHA256SUMS` is left out. Integrity is
the committed `.terraform.lock.hcl`, whose hashes Terraform checks on every
`init`. There is exactly one install mechanism: a filesystem mirror.

1. Download the release zips and `SHA256SUMS`, verify them once with
   `sha256sum -c`, and lay them out as a mirror, using the committed script
   (it also writes the CLI config in step 2):

   ```bash
   tooling/install-from-release.sh X.Y.Z /opt/xema-mirror
   ```

   The result is `/opt/xema-mirror/registry.terraform.io/xema-dev/xema/terraform-provider-xema_X.Y.Z_<os>_<arch>.zip`
   (Terraform's packed filesystem-mirror layout).

2. Commit a CLI config with a `filesystem_mirror` for this source only and
   `direct` for everything else (the script writes it to
   `/opt/xema-mirror/terraform.rc`), and point Terraform at it:

   ```hcl
   provider_installation {
     filesystem_mirror {
       path    = "/opt/xema-mirror"
       include = ["registry.terraform.io/xema-dev/xema"]
     }
     direct {
       exclude = ["registry.terraform.io/xema-dev/xema"]
     }
   }
   ```

   ```bash
   export TF_CLI_CONFIG_FILE=/opt/xema-mirror/terraform.rc
   ```

3. Pin the version in `required_providers` (`version = "= X.Y.Z"`), record the
   hashes for every platform that will run the configuration, and commit the
   lock file:

   ```bash
   terraform providers lock -fs-mirror=/opt/xema-mirror \
     -platform=linux_amd64 -platform=linux_arm64 -platform=darwin_arm64
   ```

   After that, `terraform init` fails if the installed package does not match
   the pinned hashes.

## Development

```bash
go build ./...   # compile
go vet ./...     # vet
gofmt -l .       # format check (must be empty)
go test ./...    # unit tests
```

## License

Apache License 2.0 — see [LICENSE](LICENSE).
