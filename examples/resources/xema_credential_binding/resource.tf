resource "xema_credential_binding" "openai" {
  scope_tier = "org"
  llm_provider = {
    slug     = "openai"
    base_url = "https://api.openai.com/v1"
  }
  auth_type            = "bearer"
  allowed_capabilities = ["llm.invoke"]
  allowed_actions      = ["invoke"]
  allowed_resources    = ["*"]

  secret_value   = var.openai_key # sensitive, write-only (Terraform 1.11+)
  secret_version = "2026-11"      # change together with secret_value to rotate
}

# Import by the credential broker's binding id:
#   terraform import xema_credential_binding.openai <binding-id>
