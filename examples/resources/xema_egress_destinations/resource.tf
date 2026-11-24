# One policy per installation; only the installation owner may write it.
resource "xema_egress_destinations" "installation" {
  kind = "admitted"
  grants = jsonencode([
    { kind = "host", host = "registry.internal.example", port = 443 },
  ])
}

# Import with the singleton id:
#   terraform import xema_egress_destinations.installation installation
