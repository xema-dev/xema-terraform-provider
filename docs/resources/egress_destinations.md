# xema_egress_destinations (Resource)

The installation's egress destination policy: which private-use destinations the platform may fetch from beyond the public
internet. One per installation (id `installation`), written only by the installation owner (anyone else is refused with 403).

## Example

```terraform
# One policy per installation; only the installation owner may write it.
resource "xema_egress_destinations" "installation" {
  kind = "admitted"
  grants = jsonencode([
    { kind = "host", host = "registry.internal.example", port = 443 },
  ])
}

# Import with the singleton id:
#   terraform import xema_egress_destinations.installation installation
```

## Schema

### Required

- `kind` (String) The policy kind, as biome-host defines it (for example `public-only`).

### Optional

- `grants` (String) The policy's grants, a JSON list passed through to biome-host unchanged (write it with `jsonencode`). A value that is not a list is refused before any call; biome-host validates the policy whole and names every problem on refusal.

### Read-Only

- `id` (String) Always `installation`.
- `revision` (Number) The policy's revision, advanced by each write. The server reads and sends it on every write; it is not configurable.
- `configured` (Boolean) Whether the policy has ever been written.

## Import

Only the singleton id imports:

```shell
terraform import xema_egress_destinations.installation installation
```

## Notes

- The declared policy replaces the whole policy; an unchanged policy writes nothing.
- A write that loses a compare-and-set against another writer fails with HTTP 409
  (`EGRESS_DESTINATIONS_REVISION_CONFLICT`) and writes nothing. The provider reports "Egress
  destinations changed during apply": run `terraform refresh` or plan again, then apply again.
- Destroy resets the policy to `public-only` (the server does this; there is no upstream DELETE). That
  only narrows egress. A revision conflict on destroy surfaces the same diagnostic.
