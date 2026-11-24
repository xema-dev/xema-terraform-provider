resource "xema_credential_default" "openai" {
  scope_tier = "org"
  llm_provider = {
    slug     = "openai"
    base_url = "https://api.openai.com/v1"
  }
  credential_binding_id = xema_credential_binding.openai.id
}

# Import by `org:<provider key>` or `project:<project id>:<provider key>`:
#   terraform import xema_credential_default.openai org:<provider-key>
