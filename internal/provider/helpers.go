// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// errFromDiags collapses error-level diagnostics into a single error so a
// helper can return `error` rather than threading diagnostics through.
func errFromDiags(diags diag.Diagnostics) error {
	var msgs []string
	for _, d := range diags.Errors() {
		msgs = append(msgs, d.Summary()+": "+d.Detail())
	}
	if len(msgs) == 0 {
		return nil
	}
	out := msgs[0]
	for _, m := range msgs[1:] {
		out += "; " + m
	}
	return errors.New(out)
}

// pathOf is a tiny wrapper to build an attribute path at the provider root.
func pathOf(attr string) path.Path {
	return path.Root(attr)
}

// clientFromResource extracts the configured *client.Client from a resource
// Configure request, recording a diagnostic on a type mismatch. Returns nil
// during the early Configure pass when ProviderData is not yet set.
func clientFromResource(req resource.ConfigureRequest, diags interface {
	AddError(summary, detail string)
},
) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		diags.AddError("Unexpected provider data",
			"Expected *client.Client. This is a bug in the provider; please report it.")
		return nil
	}
	return c
}

// optString returns a pointer-safe string value, treating null/unknown as "".
func optString(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

// strOrNull builds a types.String, mapping "" from the server to null so an
// optional attribute that the server omits stays null in state.
func strOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// specString reads a string field out of a server-returned spec map.
func specString(spec map[string]any, key string) string {
	if spec == nil {
		return ""
	}
	if v, ok := spec[key].(string); ok {
		return v
	}
	return ""
}

// numberFromSpec reads a numeric field out of a server-returned spec map. JSON
// numbers decode to float64, so the float64 path is the live one; the int64
// path guards against a non-default decoder.
func numberFromSpec(spec map[string]any, key string) (float64, bool) {
	if spec == nil {
		return 0, false
	}
	switch v := spec[key].(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}
