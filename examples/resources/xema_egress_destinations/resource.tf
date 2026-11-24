# One policy per installation; only the installation owner may write it.
resource "xema_egress_destinations" "installation" {
  kind = "grants"
  grants = jsonencode([
    { host = "registry.internal.example" },
  ])
}

# Import with the singleton id:
#   terraform import xema_egress_destinations.installation installation
