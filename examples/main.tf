terraform {
  required_providers {
    xema = {
      source = "xema-dev/xema"
    }
  }
}

# Endpoint / org / token may also be supplied via the
# XEMA_ENDPOINT / XEMA_ORG / XEMA_TOKEN environment variables.
provider "xema" {
  endpoint = "https://control-plane.xema.dev"
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
        model_lane = "coder"
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
resource "xema_biome_install" "slack" {
  biome_id     = "slack-connector"
  config_json  = jsonencode({ defaultChannel = "#general" })
  connections = jsonencode([{ adapterKind = "chat", orgConnectionId = "oi_123" }])
  resources    = jsonencode([{ adapterKind = "chat", selector = { team = "platform" } }])
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
