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

data "xema_project" "lookup" {
  id = xema_project.demo.id
}

output "demo_project_name" {
  value = data.xema_project.lookup.name
}
