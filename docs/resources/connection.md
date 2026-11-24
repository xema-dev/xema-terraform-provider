# xema_connection (Resource)

A connection to an external provider in the connector gateway, authenticated by an existing
credential binding named by id. The configuration and state never carry a secret.

## Example

```terraform
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
```

## Schema

### Required

- `provider_slug` (String) The external provider the connection is to. A change replaces the connection.
- `name` (String) The connection's name. A change replaces the connection.
- `scope_tier` (String) `org` or `user`. A change replaces the connection.
- `credential_kind` (String) The credential kind the connection declares; the bound binding must be of this kind. A change replaces the connection.
- `credential_binding_id` (String) The id of an existing credential binding. Updates in place.

### Optional

- `endpoint` (String) The connection's endpoint. Updates in place and re-binds the credential (an endpoint change suspends credentials). The gateway cannot clear an endpoint: removing it from the configuration keeps the live one.

### Read-Only

- `id` (String) The connector gateway's connection id.
- `endpoint_revision` (Number) The endpoint's revision, advanced by each endpoint change.
- `credentials_suspended` (Boolean) Whether the connection's credentials are suspended.

## Import

Import by the connection's id:

```shell
terraform import xema_connection.github <connection-id>
```

Import is by id only. Adopting an existing managedKey connection is unsupported: the connection
list route refuses managedKey connections (422), so the provider never lists connections.
