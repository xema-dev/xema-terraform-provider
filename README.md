# xema-terraform-provider

A [Terraform](https://www.terraform.io) provider for the [Xema](https://xema.dev)
platform — Xema-as-Code. It binds to the Xema **control-plane-api** and lets you
declare Xema platform resources as Terraform configuration, with Terraform owning
state and drift detection.

Built on the
[Terraform Plugin Framework](https://github.com/hashicorp/terraform-plugin-framework).

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

The org is also resolved authoritatively from the verified token server-side; the
provider sends the canonical `X-Xema-Org-Id` tenant header for routing parity.

## Resources

The provider exposes **all 15** resource kinds the control plane wires. Every
CRUD verb maps to the per-resource REST surface:

| Terraform                       | Kind                    | Backing service       |
| ------------------------------- | ----------------------- | --------------------- |
| `xema_project`                  | `project`               | project-registry-api  |
| `xema_provider`                 | `provider`              | llm-registry-api      |
| `xema_model`                    | `model`                 | llm-registry-api      |
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

> **`xema_org`** is operator-scoped: creating/deleting an org requires a
> platform-admin token; an org admin may only read/update their own org.
> **`xema_biome_install`** manages **org-scoped** installs (`projectId` null);
> the pinned biome version is service-managed and is not a declarable field.
> **`xema_space`** updates `classification` in place; changing `ref`,
> `display_name`, or `labels` forces replacement (the owning service's update is
> classification-only).

CRUD ↔ control-plane routes:

| Terraform op | HTTP                                              |
| ------------ | ------------------------------------------------- |
| Create       | `POST   /control-plane/resources/:kind`           |
| Read         | `GET    /control-plane/resources/:kind/:id`       |
| Update       | `PUT    /control-plane/resources/:kind/:id`       |
| Delete       | `DELETE /control-plane/resources/:kind/:id`       |

The server-minted `physicalId` is stored as the resource `id` in state.

> **Note on `xema_provider.api_key`:** the API key is write-only — the control
> plane never returns it on read, so it is not drift-detected. Secrets never
> round-trip through the control plane.

A `xema_project` **data source** is provided for read-by-id lookups.

See [`examples/main.tf`](examples/main.tf) for a complete configuration.

## Development

```bash
go build ./...   # compile
go vet ./...     # vet
gofmt -l .       # format check (must be empty)
go test ./...    # unit tests
```

## License

Apache License 2.0 — see [LICENSE](LICENSE).
