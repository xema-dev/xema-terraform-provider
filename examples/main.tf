terraform {
  required_providers {
    xema = {
      source = "xema-dev/xema"
    }
  }
}

# The provider is not on the public Terraform Registry: install it from the
# GitHub release through a filesystem mirror (README, "Installing from a release").
#
# Endpoint / org / token may also be supplied via the
# XEMA_ENDPOINT / XEMA_ORG / XEMA_TOKEN environment variables. The endpoint is
# the control-plane-api base URL behind the installation's API gateway,
# https://api.<installation-domain>/api/control-plane-api (here the dev
# installation); the provider appends /control-plane/resources/<kind> itself.
provider "xema" {
  endpoint = "https://api.dev.xema.dev/api/control-plane-api"
  org      = "org_123"
  token    = var.xema_token
}

variable "xema_token" {
  type      = string
  sensitive = true
}

resource "xema_project" "demo" {
  name        = "demo"
  description = "A project provisioned as code."
}

resource "xema_provider" "openai" {
  name     = "OpenAI"
  slug     = "openai"
  api_type = "openai"
  base_url = "https://api.openai.com/v1"
  api_key  = var.xema_token # use a dedicated secret in real usage
}

resource "xema_model" "gpt4o" {
  provider_id    = xema_provider.openai.id
  model_id       = "gpt-4o"
  display_name   = "GPT-4o"
  context_window = 128000
}

resource "xema_model" "gpt4o_mini" {
  provider_id    = xema_provider.openai.id
  model_id       = "gpt-4o-mini"
  display_name   = "GPT-4o mini"
  context_window = 128000
}

# A model strategy is a named container of rules; it holds nothing else. Bind it
# to a project and its rules apply to that project. The rules are the strategy's
# `rules` set, replaced as a whole on every apply (`rules = []` for none).
# `target_model_id` is the registry model record id (LlmModel.id). A selector
# needs at least one dimension; here the lane decides.
resource "xema_model_strategy" "house" {
  slug         = "house"
  display_name = "House strategy"

  rules = [
    {
      target_model_id         = xema_model.gpt4o.id
      target_reasoning_effort = "high"
      priority                = 20
      selector = {
        model_lane = "standard"
      }
    },
  ]
}

# An org-level rule (no strategy): most-specific match wins.
resource "xema_model_resolution_rule" "by_project" {
  target_model_id = xema_model.gpt4o_mini.id
  priority        = 10

  selector = {
    project = xema_project.demo.id
    stage   = "review"
  }
}

# The organization default model: used when nothing more specific applies.
# Never substituted; unset, an unresolved invocation fails MODEL_NOT_CONFIGURED.
resource "xema_org_model_default" "this" {
  model_id = xema_model.gpt4o_mini.id
}

# The organization's external directory binding: members are the holders of the
# claim values, and each value maps to an org role.
resource "xema_directory_binding" "this" {
  provider_ref          = "entra:00000000-0000-0000-0000-000000000000"
  application_client_id = "11111111-1111-1111-1111-111111111111"

  member_claim = {
    kind   = "GROUP"
    values = ["22222222-2222-2222-2222-222222222222"]
  }

  role_map = [
    { value = "22222222-2222-2222-2222-222222222222", org_role = "org_member" },
  ]
}

# The installation's public exposure (installation owner only). Destroying it
# leaves the installation setting as last written.
resource "xema_public_exposure" "this" {
  entries = [
    {
      host        = "api.example.com"
      path_prefix = "/api"
      select      = { kind = "all", deny = ["/billing/**"] }
    },
    {
      host         = "example.com"
      path_prefix  = "/api"
      select       = { kind = "globs", allow = ["/users/*"] }
      cors_origins = ["https://app.example.com"]
    },
  ]
}

# A custom org role (permission set).
resource "xema_role" "kb_editor" {
  slug         = "kb-editor"
  display_name = "Knowledge Base Editor"
  description  = "Can edit knowledge-base deliverables."
}

# A custom role holding the consent-decide grant, so a pipeline's service
# identity can approve the permissions a biome install asks for. `grants` is
# the role's complete grant set: leaving it unset declares none.
resource "xema_role" "consent_decider" {
  slug         = "installation-consent-decider"
  display_name = "Installation consent decider"
  description  = "May decide installation permission consent for this org."

  grants = [
    {
      capability  = "authorization:installation-consent.decide@1"
      environment = "org"
    },
  ]
}

variable "pipeline_principal_id" {
  type        = string
  description = "Principal id of the pipeline's service identity."
}

# The pipeline's service identity holds that role. Apply this with a person's
# credential (an org admin): assigning roles refuses a service token by design.
resource "xema_role_assignment" "pipeline_consent_decider" {
  role_id      = xema_role.consent_decider.id
  subject_kind = "service"
  subject_ref  = var.pipeline_principal_id
}

# A custom role holding the biome-install grant: a pipeline's service identity
# must hold `biome:install@1` in the org to be admitted to `xema_biome_install`
# at all (refused with `service-principal-lacks-install` otherwise).
resource "xema_role" "biome_installer" {
  slug         = "biome-installer"
  display_name = "Biome installer"
  description  = "May install biomes in this org."

  grants = [
    {
      capability  = "biome:install@1"
      environment = "org"
    },
  ]
}

# The pipeline's service identity holds that role too (a person's credential
# applies this, like the assignment above).
resource "xema_role_assignment" "pipeline_biome_installer" {
  role_id      = xema_role.biome_installer.id
  subject_kind = "service"
  subject_ref  = var.pipeline_principal_id
}

# A pipeline's service principal. The client secret is returned once and is
# stored in Terraform state as a sensitive value: keep the state backend
# encrypted and access-restricted, and write the secret straight into the CI
# secret store (a github_actions_secret or Key Vault secret resource), never
# into an output. Bump rotation_trigger to rotate the secret in place.
#
# The pipeline turns client_id / client_secret into the provider's XEMA_TOKEN
# with the client-credentials grant at token_endpoint, and must request
# `scope=audience:platform` (the control plane's audience). The principal's
# client holds that audience only as an optional scope, so a token minted
# without it is refused with 401 (README, "A pipeline's service-principal token").
resource "xema_service_principal" "pipeline" {
  name             = "ci-pipeline"
  display_name     = "CI pipeline"
  rotation_trigger = "2026-11"
}

# An organization. Create/update/delete is an installation operation and
# requires a platform-admin token (org admins read their own org via the data source).
resource "xema_org" "acme" {
  name         = "acme-corp"
  display_name = "Acme Corporation"
  domain       = "acme.example.com"
  metadata     = jsonencode({ tier = "enterprise" })
}

# A reusable deliverable spec (document template).
resource "xema_deliverable_spec" "adr" {
  slug     = "architecture-decision-record"
  version  = "1.0.0"
  title    = "Architecture Decision Record"
  kind     = "DOCUMENT_TEMPLATE"
  category = "architecture"
  tags     = ["adr", "architecture"]
  content  = "# {{title}}\n\n## Context\n\n## Decision\n\n## Consequences\n"
}

# An org-scoped biome installation (projectId null; version is service-managed).
# accept_permissions consents to the grant set the install plan shows; unset, an
# install whose plan needs consent is refused before it runs. It acts only when
# the install is created. Run from a pipeline, the provider's token can be the
# pipeline's own service-principal token (with `org` set): that service must
# hold the biome-installer role (`biome:install@1`) to be admitted at all, and
# the consent-decider role to give consent; both are assigned above.
# config_json, connections and resources are optional: unset means none.
resource "xema_biome_install" "slack" {
  biome_id           = "slack-connector"
  config_json        = jsonencode({ defaultChannel = "#general" })
  connections        = jsonencode([{ adapterKind = "chat", orgConnectionId = "oi_123" }])
  resources          = jsonencode([{ adapterKind = "chat", selector = { team = "platform" } }])
  accept_permissions = true
}

# A biome that takes no configuration and binds nothing names only the biome.
resource "xema_biome_install" "no_config" {
  biome_id           = "example-biome"
  accept_permissions = true
}

# An org portal (an org-scoped app). Opaque blobs are normalized JSON.
resource "xema_portal" "ops" {
  slug              = "ops"
  display_name      = "Operations"
  default_zone      = "default"
  branding          = jsonencode({ primaryColor = "#0A66C2" })
  lockfile          = jsonencode({ kernel = "1.0.0" })
  installed_biomes  = jsonencode([])
  capability_policy = jsonencode([])
}

data "xema_project" "lookup" {
  id = xema_project.demo.id
}

# A service-managed project is resolved by its managed key instead of an id.
data "xema_project" "by_key" {
  managed_key = "forge:gim"
}

output "demo_project_name" {
  value = data.xema_project.lookup.name
}
