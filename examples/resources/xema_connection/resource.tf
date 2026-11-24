# The credential is an existing credential binding, referenced by id only.
resource "xema_credential_binding" "github" {
  scope_tier = "org"
  llm_provider = {
    slug     = "github"
    base_url = "https://api.github.com"
  }
  auth_type            = "bearer"
  allowed_capabilities = ["llm.invoke"]
  allowed_actions      = ["invoke"]
  allowed_resources    = ["*"]
  secret_value         = var.github_token # sensitive, write-only
}

resource "xema_connection" "github" {
  provider_slug         = "github"
  name                  = "github-main"
  scope_tier            = "org"
  credential_kind       = "model_provider"
  endpoint              = "https://api.github.com"
  credential_binding_id = xema_credential_binding.github.id
}

# Import by the gateway's connection id:
#   terraform import xema_connection.github <connection-id>
