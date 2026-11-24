# xema_org_setting (Resource)

The organisation's own (admin) value of one platform setting, managed through space-registry via the control plane.
Destroying the resource clears the value, so the setting reverts to its declaration or default.

## Example

```terraform
# The value is a JSON scalar in wire form: write it with jsonencode.
resource "xema_org_setting" "session_timeout" {
  key   = "ns:session-timeout@1"
  value = jsonencode("PT5M") # a duration is an ISO-8601 string
}

# Import by the setting key:
#   terraform import xema_org_setting.session_timeout 'ns:session-timeout@1'
```

## Schema

### Required

- `key` (String) The setting key, `<namespace>:<name>@<major>`. A changed key replaces the resource.
- `value` (String) The value in wire form, as a JSON scalar: a boolean, a number or a string (a duration is an ISO-8601 string). Write it with `jsonencode`, for example `jsonencode(true)` or `jsonencode("PT5M")`. An object, list or null is refused before any call.

### Read-Only

- `id` (String) The setting key (the control-plane physical id).
- `revision` (Number) The setting's revision, advanced by each write.

## Import

Import by the setting key:

```shell
terraform import xema_org_setting.session_timeout 'ns:session-timeout@1'
```

## Notes

- Create and update are the same write upstream; the value is read back after each write, and the
  declared spelling is kept when the server value is semantically equal JSON.
- A setting the deployment declares refuses the write (403).
- Destroy clears the organisation's value; a setting already gone is a successful destroy.
