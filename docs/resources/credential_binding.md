# xema_credential_binding (Resource)

An organisation- or project-tier `model_provider` credential binding in the credential broker: the sealed key for one LLM
provider endpoint, with the allow-lists that bound its use. The configuration and state never carry the secret.

## Example

```terraform
variable "openai_key" {
  type      = string
  sensitive = true
}

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
```

## Schema

### Required

- `scope_tier` (String) `org` or `project`. A change replaces the binding.
- `llm_provider` (Attributes) The LLM provider the binding's key is derived from. Config-only: the server does not return it, so it is not refreshed. A change replaces the binding (a resource imported without it adopts the declared value in place).
  - `slug` (String) The LLM provider slug.
  - `base_url` (String) The provider's inference endpoint base URL.
- `auth_type` (String) How the endpoint takes the key: `bearer` or `api_key`. A change replaces the binding.
- `allowed_capabilities` (List of String) Capabilities the binding may be used for (non-empty).
- `allowed_actions` (List of String) Actions the binding may be used for (non-empty).
- `allowed_resources` (List of String) Resources the binding may be used on (non-empty).
- `secret_value` (String, Sensitive, Write-only) The provider key. Read from the configuration only, never stored in the plan or state, and never returned by the service. Needs Terraform 1.11 or later.

### Optional

- `project_id` (String) The project id; required for `project`, refused for `org`. A change replaces the binding.
- `header_name` (String) The header carrying the key; required for `api_key`, refused for `bearer`. Updates in place.
- `secret_version` (String) A label you choose for the secret. Change it, together with `secret_value`, to rotate the secret in place.

### Read-Only

- `id` (String) The credential broker's binding id.
- `provider_key` (String) The provider key the server derived from `llm_provider`.
- `version` (Number) The binding's version, advanced by each in-place change or rotation.

## Import

Import by the broker's binding id:

```shell
terraform import xema_credential_binding.openai <binding-id>
```

## Notes

- `secret_value` is sent on create and only when `secret_version` changes. Changing `secret_value`
  alone never rotates the secret, and an ordinary re-apply never rotates.
- The allow-lists and `header_name` update in place; scope, project, auth type and provider replace.
- Destroy revokes the binding; a binding already revoked or gone (404) is a successful destroy.
- Pair it with `xema_credential_default` to make it the default for the provider, or reference its id
  from `xema_connection`.
