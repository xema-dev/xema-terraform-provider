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

# Default rule (empty selector) resolving to a specific model.
resource "xema_model_resolution_rule" "default" {
  target_kind     = "model"
  target_model_id = "gpt-4o"
  is_default      = true
}

# A dimensional rule: most-specific match wins.
resource "xema_model_resolution_rule" "by_project" {
  target_kind     = "model"
  target_model_id = "gpt-4o-mini"
  priority        = 10

  selector = {
    project = xema_project.demo.id
    stage   = "review"
  }
}

# A custom org role (permission set).
resource "xema_role" "kb_editor" {
  slug         = "kb-editor"
  display_name = "Knowledge Base Editor"
  description  = "Can edit knowledge-base deliverables."
}

# An organization. Create/delete requires a platform-admin (operator) token.
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
  integrations = jsonencode([{ adapterKind = "chat", orgIntegrationId = "oi_123" }])
  resources    = jsonencode([{ adapterKind = "chat", selector = { team = "T123" } }])
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

output "demo_project_name" {
  value = data.xema_project.lookup.name
}
