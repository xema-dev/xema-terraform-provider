# Changelog

## Unreleased

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
