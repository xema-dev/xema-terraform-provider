// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"net/http"
)

// writeData answers as control-plane-api does: every success payload sits under
// {"data": ...}.
func writeData(w http.ResponseWriter, payload any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": payload})
}
