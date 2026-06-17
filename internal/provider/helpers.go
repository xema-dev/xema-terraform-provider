// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
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

// normalizedToValue parses a jsontypes.Normalized JSON blob (object OR array)
// into a Go value for inclusion in a spec. A null/unknown/empty value yields a
// nil value so the field is omitted from the spec.
func normalizedToValue(v jsontypes.Normalized) (any, error) {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal([]byte(v.ValueString()), &out); err != nil {
		return nil, fmt.Errorf("parse JSON value: %w", err)
	}
	return out, nil
}

// normalizedFromValue serializes an arbitrary Go value (object or array) into a
// jsontypes.Normalized, mirroring specNormalized for a value not keyed in a spec
// map. A nil value yields a normalized null.
func normalizedFromValue(v any) (jsontypes.Normalized, error) {
	if v == nil {
		return jsontypes.NewNormalizedNull(), nil
	}
	buf, err := json.Marshal(v)
	if err != nil {
		return jsontypes.NewNormalizedNull(), fmt.Errorf("serialize value: %w", err)
	}
	return jsontypes.NewNormalizedValue(string(buf)), nil
}

// specNormalized serializes a server-returned spec field (object or array) into
// a jsontypes.Normalized. encoding/json sorts object keys deterministically, and
// jsontypes compares semantically, so the round-trip is drift-free. A missing or
// null field yields a normalized null.
func specNormalized(spec map[string]any, key string) jsontypes.Normalized {
	raw, ok := spec[key]
	if !ok || raw == nil {
		return jsontypes.NewNormalizedNull()
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return jsontypes.NewNormalizedNull()
	}
	return jsontypes.NewNormalizedValue(string(buf))
}

// stringList reads a []string field out of a server-returned spec map.
func stringList(spec map[string]any, key string) []string {
	raw, ok := spec[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
