# Changelog

## Unreleased

- XEMA-11489: **Breaking:** `xema_role` and `xema_role_assignment` follow the
  control plane's move to authorization-api role definitions and bindings, which
  refuses the old fields (400 `ROLE_SPEC_FIELD_RETIRED` and
  `ROLE_ASSIGNMENT_SPEC_FIELD_RETIRED`).
  - `xema_role`: `grants` is removed; the required `pattern` (a JSON object,
    `{ clauses: [{ actions, where?, except?, bound? }] }`, write it with
    `jsonencode`) replaces it. The `xema_role` data source exposes `pattern`
    instead of `grants`. `slug` still replaces the resource.
  - `xema_role_assignment`: `role_id`, `subject_kind` and `subject_ref` are
    removed. Use `definition_id` (the `xema_role` id), `principal = { kind, ref }`
    and the optional `scope_ref`, `environment_ref` and `until`. A binding has no
    update, so any change replaces it; the id is now the binding id.
  - Upgrade: state for old roles and assignments reads as gone. Run
    `terraform state rm` on each `xema_role` and `xema_role_assignment`, then
    re-create them from the new configuration (or `terraform import` them by id).
    The `xema_role` and `xema_role_assignment` schemas also changed, so rewrite
    the configuration before `plan`.
- XEMA-11420: `xema_role_assignment.subject_kind` accepts `team` and no longer
  accepts `group`, following authorization-api's SubjectKind (XEMA-10246 renamed
  Group to Team; the server refuses `group` with 422 RESOURCE_SPEC_INVALID, so a
  `group` assignment never worked). Breaking for a configuration that names
  `group`: write `team`.
- XEMA-10608: `xema_service_principal` (resource and data source) exports the
  computed `audience`, the one audience the client credential is bound to, as
  the service reports it (create, read, rotation). Request tokens with
  `scope=audience:<audience>`. It is null when the service reports none (or is
  older). Needs no new server version; an older service leaves it null.
- Raises terraform-plugin-framework to 1.19.0 (write-only attributes) and the
  Go directive to 1.25.0.
- XEMA-10065: `xema_org_setting`, `xema_credential_binding`,
  `xema_credential_default` and `xema_egress_destinations`, each with a data
  source. `xema_credential_binding.secret_value` is a sensitive write-only
  attribute (never in plan or state; needs Terraform 1.11 or later), sent only on
  create and when `secret_version` changes. Needs control-plane
  with these resource kinds (xema-base XEMA-10065).
- XEMA-10065: `xema_connection` (a gateway connection bound to an existing
  `xema_credential_binding` by `credential_binding_id`; import by id only).
- `xema_provider.api_key` is deprecated: it retires once keys are enrolled
  through `xema_credential_binding` + `xema_credential_default`. It is not
  removed yet.
- **Breaking:** `xema_grant` and `data.xema_grant` are removed. Declare a
  `xema_role` with the grants and a `xema_role_assignment` instead. Before you
  upgrade, run `terraform state rm` on every `xema_grant` and
  `data.xema_grant` address (README, "Breaking changes in v0.6.0").
- `xema_service_principal`: an OAuth client for a CI pipeline (`client_id`,
  `client_secret`, `token_endpoint`). The service returns the secret once, on
  create and on each in-place rotation (`rotation_trigger`); Terraform keeps it
  in state, so the state backend holds a secret. Applying it needs a person's
  org-admin token; a service token is refused. Needs control-plane with the
  service-principal resource kind (xema-base opus-9599b).
- `xema_biome_install` gains `accept_permissions`, and its consent and spec
  refusals are explained. Requires control-plane with the biome-install consent
  sequence (xema-base x9540-u2b/u2c). Publish this release only after that is
  deployed.
- `xema_biome_install.biome_id` now plans a replacement.
- `xema_biome_install` explains a consent refusal by its closed `reason` and
  typed `stage`: `after-run` (or `after-run-consent-not-given`) says the
  installation exists unconfigured and to import or remove it; no stage says
  the outcome is unknown, never that nothing ran. `not-authorized`,
  `installation-read-not-authorized`, `not-authenticated`, `unavailable` and
  the moved or settled reasons name their own remedy; others are quoted as
  sent. `CONTROL_PLANE_BIOME_INSTALL_CONSENT_REFUSAL_UNMAPPED` (502) asks to
  report it and follows the same stage rule. Needs xema-base x9540-polish and
  x9540-decide-reasons.
