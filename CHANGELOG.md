# Changelog

## Unreleased

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
