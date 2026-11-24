# xema_credential_default (Resource)

The organisation's or a project's default credential for one LLM provider: a pointer to a `xema_credential_binding`,
written through space-registry as the caller.

## Example

```terraform
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
```

## Schema

### Required

- `scope_tier` (String) `org` or `project`. A change replaces the default.
- `llm_provider` (Attributes) The LLM provider the default is for. Config-only: the server keeps only the derived provider key. A change replaces the default.
  - `slug` (String) The LLM provider slug.
  - `base_url` (String) The provider's inference endpoint base URL.
- `credential_binding_id` (String) The id of the `xema_credential_binding` that is the default. A change repoints the default in place.

### Optional

- `project_id` (String) The project id; required for `project`, refused for `org`. A change replaces the default.

### Read-Only

- `id` (String) `org:<provider key>` or `project:<project id>:<provider key>`.
- `provider_key` (String) The provider key the server derived from `llm_provider`.

## Import

Import by the id; any other shape is refused:

```shell
terraform import xema_credential_default.openai org:<provider-key>
terraform import xema_credential_default.openai project:<project-id>:<provider-key>
```

## Notes

- Destroy clears the default, so resolution falls through to the next rung; a default already gone is a successful destroy.
