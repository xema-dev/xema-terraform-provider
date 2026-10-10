# xema-terraform-provider

A [Terraform](https://www.terraform.io) provider for the [Xema](https://xema.dev)
platform — Xema-as-Code. It binds to the Xema **control-plane-api** and lets you
declare Xema platform resources as Terraform configuration, with Terraform owning
state and drift detection.

## Provider configuration

The provider talks to a single control-plane endpoint, for a single org, with an
org-admin bearer token. Each attribute has an environment-variable fallback.

| Attribute  | Env var          | Description                                   |
| ---------- | ---------------- | --------------------------------------------- |
| `endpoint` | `XEMA_ENDPOINT`  | Control-plane-api base URL.                   |
| `org`      | `XEMA_ORG`       | Xema organization id.                         |
| `token`    | `XEMA_TOKEN`     | Org-admin bearer token (sensitive).           |

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
> platform-admin token (the configured `org` is only the request's transport context). Installation authority
> never implies org admin, and vice versa. An org admin reads their own org via
> the `xema_org` data source. (The provider `org` attribute is still required.)
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

### `data.xema_distribution_lock` (operator plane)

Resolves a Distribution and an available-biome index into a `DistributionLock`
through fleet-control-api. It has no side effects, so it is a data source. It
needs the provider's `fleet_endpoint` (or `XEMA_FLEET_ENDPOINT`) and a service
token, not a plain org-admin token.

All four inputs are required; fewer is a `422`.

| Input | What it is |
|---|---|
| `distribution` | the `Distribution` to resolve (`schemaVersion`, `id`, `include[]`, ...) |
| `available_biomes` | the biome index to resolve against, as an array of `AvailableBiome` objects |
| `capability_domain_owners` | the platform-reserved `{domain, biomeId}` table, non-empty |
| `platform_service_sources` | exact `{name, tier, repo, commit, version, sourceManifestHash}` per declared platform service |

The output `lock` is the resolved `DistributionLock` (schema version 2).

## Development

```bash
go build ./...   # compile
go vet ./...     # vet
gofmt -l .       # format check (must be empty)
go test ./...    # unit tests
```

## License

Apache License 2.0 — see [LICENSE](LICENSE).
