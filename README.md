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
| `token`    | `XEMA_TOKEN`     | Bearer token (sensitive): org-admin, or platform-admin in operator mode. A pipeline's own service-principal token works for `xema_biome_install` (see below). |

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

### A pipeline's service-principal token

A pipeline can apply `xema_biome_install` with its own service-principal token
instead of a person's: set `org` (the provider sends it as `X-Xema-Org-Id`) and
pass the token through `XEMA_TOKEN` from the pipeline's secret store, never in
the configuration. The control plane admits it for biome installs only; every
other kind still needs an org-admin person token, and assigning roles refuses a
service token by design. The service needs two grants in that org:

- `biome:install@1`, to be admitted at all (without it the install is refused
  with `service-principal-lacks-install`);
- `authorization:installation-consent.decide@1`, held through a role, to give
  consent (`accept_permissions = true`).

An org admin declares the roles and their `xema_role_assignment`s once, with a
person's credential.

```hcl
# In the pipeline: XEMA_ENDPOINT, XEMA_ORG and XEMA_TOKEN (the service token) come from the environment.
provider "xema" {}

resource "xema_biome_install" "slack" {
  biome_id           = "slack-connector"
  config_json        = jsonencode({})
  connections        = jsonencode([])
  resources          = jsonencode([])
  accept_permissions = true
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

Each resource kind below has a matching data source (`data.xema_<kind>`) that reads by `id`. `data.xema_project` also accepts `managed_key` (exactly one of the two), e.g. `data "xema_project" "gim" { managed_key = "forge:gim" }`; it requires exactly one matching project in the org and errors on none or several.

| Terraform                       | Kind                    | Backing service       |
| ------------------------------- | ----------------------- | --------------------- |
| `xema_project`                  | `project`               | project-registry-api  |
| `xema_provider`                 | `provider`              | llm-registry-api      |
| `xema_model`                    | `model`                 | llm-registry-api      |
| `xema_model_strategy`           | `model-strategy`        | llm-registry-api      |
| `xema_model_resolution_rule`    | `model-resolution-rule` | llm-registry-api      |
| `xema_org_model_default`        | `org-model-default`     | llm-registry-api      |
| `xema_directory_binding`        | `directory-binding`     | identity-api          |
| `xema_agent`                    | `agent`                 | llm-registry-api      |
| `xema_skill`                    | `skill`                 | skill-registry-api    |
| `xema_role`                     | `role`                  | authorization-api     |
| `xema_role_assignment`          | `role-assignment`       | authorization-api     |
| `xema_service_principal`        | `service-principal`     | identity-api          |
| `xema_team`                     | `team`                  | authorization-api     |
| `xema_environment`              | `environment`           | authorization-api     |
| `xema_space`                    | `space`                 | space-registry-api    |
| `xema_org`                      | `org`                   | identity-api          |
| `xema_deliverable_spec`         | `deliverable-spec`      | deliverable-specs-api |
| `xema_biome_install`            | `biome-install`         | biome-host-api        |
| `xema_portal`                   | `portal`                | app-runtime-api       |
| `xema_public_exposure`          | `public-exposure`       | biome-host-api        |
| `xema_org_setting`              | `org-setting`           | space-registry-api    |
| `xema_credential_binding`       | `credential-binding`    | credential-broker-api |
| `xema_credential_default`       | `credential-default`    | space-registry-api    |
| `xema_egress_destinations`      | `egress-destinations`   | biome-host-api        |

Opaque object/array fields (e.g. `metadata`, `config_json`, `branding`,
`lockfile`, `integrations`, `resources`, `access_grants`) are typed as
normalized JSON strings — Terraform compares them semantically, so formatting
or key-order differences never show as drift.

> **`xema_org`** (resource) is installation-scoped: creating, updating and
> deleting an org uses the control-plane installation routes, needs a
> platform-admin token, and works in operator mode (no `org`). Installation authority
> never implies org admin, and vice versa. An org admin reads their own org via
> the `xema_org` data source.
> **`xema_model_strategy`** is a named container of rules: its `rules` set is the
> strategy's complete rule set, replaced as a whole on each apply (`rules = []`
> for none). The declared `rules` set is authoritative: apply replaces the
> strategy's rules, including any change made since Terraform's refresh. Only
> the control plane's own read-write window is guarded by the strategy's version.
> Each rule targets a model id (`target_model_id`) and has the same body as
> **`xema_model_resolution_rule`**, which declares an org-level rule only.
> **`xema_org_model_default`** is the organization's one default model: one per
> org, set by `model_id`, and destroying it unsets the default.
> **`xema_directory_binding`** is the organization's one external-directory binding
> (id is the org id; import with it): `provider_ref`, `application_client_id`, a
> `member_claim` (`GROUP` or `APP_ROLE`, with at least one value) and an optional
> `role_map` of claim value to org role (`org_owner`, `org_admin`, `org_member`).
> Any change updates it in place; an empty `role_map` reads as unset, and
> destroying it removes the binding but leaves memberships untouched.
> **`xema_public_exposure`** (and `data.xema_public_exposure`, which takes no `id`) is the installation's one public-exposure setting
> (id `installation`; import with that id): the hosts and path prefixes that
> expose public operations, and their CORS origins. Only the installation owner
> may write it (403 otherwise). The declared `entries` replace the whole
> document, an unchanged document writes nothing, and `revision` / `configured`
> are read-only. Destroying it only stops Terraform managing the setting: the
> installation's exposure is left as last written, never reset.
> **`xema_org_setting`** is the organisation's own value of one setting (id and
> import id: the setting key `<namespace>:<name>@<major>`). `value` is a JSON
> scalar, written with `jsonencode`. Destroying it clears the value, so the
> setting reverts to its declaration or default.
> **`xema_credential_binding`** is a sealed LLM provider key with its allow-lists
> (id: the broker's binding id; import with it). `secret_value` is sensitive and
> write-only: it is read from the configuration only and never stored in plan or
> state (needs Terraform 1.11 or later). It is sent on create and when
> `secret_version` changes, so a re-apply never rotates; change both to rotate.
> `llm_provider` is config-only (never read back).
> A changed scope, project, auth type or provider replaces the binding; destroying
> it revokes it, and an already revoked binding destroys cleanly.
> **`xema_credential_default`** points an org's or project's provider at a
> binding (id `org:<provider key>` or `project:<project id>:<provider key>`).
> Destroying it clears the default. **`xema_provider.api_key`** is deprecated:
> enrol keys through these two resources instead.
> **`xema_egress_destinations`** is the installation's egress policy (id
> `installation`; owner-only): `kind` and `grants`, the grants a JSON list passed
> to biome-host unchanged. The server reads and sends the revision, so it is not
> configurable; a concurrent write fails with a clear conflict message. Destroying
> it resets the policy to `public-only`.
> **`xema_biome_install`** manages **org-scoped** installs (`projectId` null);
> the pinned biome version is service-managed and is not a declarable field, and
> changing `biome_id` replaces the install. The control plane plans the install,
> gives the grant consent the plan asks for when `accept_permissions = true`
> (through the credential's own consent door), and runs the install bound to
> that plan. Unset means false: an install whose plan needs consent is then
> refused before it runs (`CONTROL_PLANE_BIOME_INSTALL_CONSENT_REFUSED`, with its
> stop reason), and the error says to set `accept_permissions = true`. A grant
> set that changed since it was planned is never approved unseen; review it and
> apply again. The refusal's `stage` says whether the install ran: `after-run`
> (or the reason `after-run-consent-not-given`) means its installation exists
> but is unconfigured, and the error says to import or remove it; with no stage
> (an older control plane) the outcome is unknown, so check whether the
> installation exists. Reasons name their remedy: `not-authorized` (give the
> credential a role holding `authorization:installation-consent.decide@1`),
> `installation-read-not-authorized` (allow it to read the org's biome
> installations on Biome Host), `not-authenticated` (renew the token),
> `unavailable` (retry), and a consent that moved or was settled (apply again);
> any other reason is quoted as sent.
> `CONTROL_PLANE_BIOME_INSTALL_CONSENT_REFUSAL_UNMAPPED` (502) is a consent
> refusal control-plane could not map: report it; its stage reads the same way.
> `accept_permissions` acts only when the install is created: changing it on an
> existing install updates nothing. It is intent only: it is never read back, so
> it never reads as drift, an import leaves it unset, and changing it never
> replaces the install. The `xema_biome_install` data source does not expose it.
> **`xema_role`** declares a custom role and its complete `grants` set (each a
> `capability` such as `authorization:installation-consent.decide@1`, an
> `environment` such as `org`, and optional `requires_approval`, `constraints`
> and `where`). The provider always sends `grants`: leaving it unset declares
> the empty set, so apply withdraws every grant the role holds, including ones
> added outside Terraform. Who holds the role is not part of it.
> **`xema_role_assignment`** assigns one role to one subject in the org
> (`role_id`, `subject_kind` such as `service` or `user`, `subject_ref`). Every
> attribute replaces it; its id is `<role_id>:<subject_kind>:<subject_ref>`
> (import with that), and destroying one that is already gone succeeds. The
> credential running Terraform must be a person's (an org admin who may confer
> the role): the role-assignment route refuses a service token by design, so a
> pipeline never assigns roles, its own included.
> **`xema_service_principal`** creates a machine identity with an OAuth client
> (`client_id`, `token_endpoint`, `client_secret`). `name` (lowercase, digits and
> `-`, at most 40 characters), `display_name` and `description` replace it on any
> change; changing `rotation_trigger` rotates the client secret in place (the old
> one stays valid for `rotation_overlap_seconds`, 30 to 86400, default 300).
> **The client secret is returned by the service once and IS stored in Terraform
> state as a sensitive value, so the state backend is a secret store: encrypt it
> and restrict access to it.** Write the secret straight into your CI secret store
> (for example a `github_actions_secret` or a Key Vault secret resource), not into
> an output. Give a pipeline's principal roles that confer no authority: assigning
> roles needs a person's token (see `xema_role_assignment`). Import by principal
> id; an imported principal has a null `client_secret`, so rotate to obtain one.
> A create or rotation that returns no secret is an error, never an empty secret.
> **`xema_space`** updates `classification` in place; changing `ref`,
> `display_name`, or `labels` forces replacement (the owning service's update is
> classification-only).

> **Note on `xema_provider.api_key`:** the API key is write-only — the control
> plane never returns it on read, so it is not drift-detected. It enrols the
> key as the organisation's default credential for the provider's endpoint; the
> platform seals it and never stores it on the provider.

Every kind also has a matching **data source** (`data.xema_<kind>`) for
read-by-id lookups: the `id` is the only input and every spec field is exposed
as a computed attribute.

### Breaking changes in v0.6.0

`xema_grant` and `data.xema_grant` are removed. Declare a `xema_role` with the
capability in its `grants` and assign it with `xema_role_assignment`. Before
upgrading, run `terraform state rm` on every `xema_grant` resource and
`data.xema_grant` address, or the new provider cannot read the old state.

`xema_service_principal` needs a person's org-admin token: a service token is
refused. A principal still `REVOKING` keeps its name, so re-creating the same
name right after a destroy can conflict until revocation finishes.

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

`SHA256SUMS` is published next to the zips, so it guards transport only, not
the origin. The trust anchor is the first `terraform providers lock` (step 3),
reviewed when the lock file is committed.

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
