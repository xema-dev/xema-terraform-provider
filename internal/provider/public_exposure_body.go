// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// publicExposureKind is the XemaResourceKind wire value of the installation's
// public-exposure setting.
const publicExposureKind = "public-exposure"

// publicExposureID is the setting's one physical id: the installation has a
// single public-exposure document, so there is nothing for an author to name.
const publicExposureID = "installation"

// publicExposureSelectKinds is the closed set of `select.kind` values
// (biome-host's PublicExposureSelectKind).
var publicExposureSelectKinds = []string{"all", "biomes", "globs"}

// publicExposureSelectModel mirrors biome-host's PublicExposureSelectDto.
type publicExposureSelectModel struct {
	Kind   types.String `tfsdk:"kind"`
	Biomes types.List   `tfsdk:"biomes"`
	Allow  types.List   `tfsdk:"allow"`
	Deny   types.List   `tfsdk:"deny"`
}

// publicExposureEntryModel mirrors biome-host's PublicExposureEntryDto.
type publicExposureEntryModel struct {
	Host        types.String              `tfsdk:"host"`
	PathPrefix  types.String              `tfsdk:"path_prefix"`
	Select      publicExposureSelectModel `tfsdk:"select"`
	CorsOrigins types.List                `tfsdk:"cors_origins"`
}

// stringListToSpec returns the declared strings of an optional list, or nil
// when it is null or unknown so the field is omitted from the document.
func stringListToSpec(l types.List) []any {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	out := make([]any, 0, len(l.Elements()))
	for _, e := range l.Elements() {
		out = append(out, e.(types.String).ValueString())
	}
	return out
}

// specToStringList reads an optional string list out of a read-back document: an
// absent field is null (not an empty list), so an omitted optional stays null.
func specToStringList(spec map[string]any, key string) types.List {
	raw, ok := spec[key].([]any)
	if !ok {
		return types.ListNull(types.StringType)
	}
	elems := make([]attr.Value, 0, len(raw))
	for _, v := range raw {
		s, _ := v.(string)
		elems = append(elems, types.StringValue(s))
	}
	return types.ListValueMust(types.StringType, elems)
}

func entryToSpec(e publicExposureEntryModel) map[string]any {
	sel := map[string]any{"kind": e.Select.Kind.ValueString()}
	for key, l := range map[string]types.List{
		"biomes": e.Select.Biomes, "allow": e.Select.Allow, "deny": e.Select.Deny,
	} {
		if v := stringListToSpec(l); v != nil {
			sel[key] = v
		}
	}
	entry := map[string]any{
		"host":       e.Host.ValueString(),
		"pathPrefix": e.PathPrefix.ValueString(),
		"select":     sel,
	}
	if v := stringListToSpec(e.CorsOrigins); v != nil {
		entry["corsOrigins"] = v
	}
	return entry
}

func entryFromSpec(raw any) publicExposureEntryModel {
	entry, _ := raw.(map[string]any)
	sel, _ := entry["select"].(map[string]any)
	return publicExposureEntryModel{
		Host:       types.StringValue(specString(entry, "host")),
		PathPrefix: types.StringValue(specString(entry, "pathPrefix")),
		Select: publicExposureSelectModel{
			Kind:   types.StringValue(specString(sel, "kind")),
			Biomes: specToStringList(sel, "biomes"),
			Allow:  specToStringList(sel, "allow"),
			Deny:   specToStringList(sel, "deny"),
		},
		CorsOrigins: specToStringList(entry, "corsOrigins"),
	}
}

// selectKindValidator refuses a `select.kind` outside the closed set at plan
// time. It is written against the framework's validator interface directly:
// this provider does not depend on terraform-plugin-framework-validators.
type selectKindValidator struct{}

func (selectKindValidator) Description(context.Context) string {
	return "value must be one of: " + strings.Join(publicExposureSelectKinds, ", ")
}

func (v selectKindValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (selectKindValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueString()
	for _, k := range publicExposureSelectKinds {
		if got == k {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Invalid select kind",
		fmt.Sprintf("`select.kind` must be one of %s; got %q.", strings.Join(publicExposureSelectKinds, ", "), got))
}
