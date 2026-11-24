# The value is a JSON scalar in wire form: write it with jsonencode.
resource "xema_org_setting" "session_timeout" {
  key   = "ns:session-timeout@1"
  value = jsonencode("PT5M") # a duration is an ISO-8601 string
}

# Import by the setting key:
#   terraform import xema_org_setting.session_timeout 'ns:session-timeout@1'
