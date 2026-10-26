// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

func strList(vals ...string) types.List {
	elems := make([]types.String, 0, len(vals))
	for _, v := range vals {
		elems = append(elems, types.StringValue(v))
	}
	l, _ := types.ListValueFrom(context.Background(), types.StringType, elems)
	return l
}

func declaredExposure() publicExposureModel {
	null := types.ListNull(types.StringType)
	return publicExposureModel{Entries: []publicExposureEntryModel{
		{
			Host: types.StringValue("api.example.com"), PathPrefix: types.StringValue("/api"),
			Select:      publicExposureSelectModel{Kind: types.StringValue("all"), Biomes: null, Allow: null, Deny: strList("/billing/**")},
			CorsOrigins: strList("https://app.example.com"),
		},
		{
			Host: types.StringValue("example.com"), PathPrefix: types.StringValue("/api"),
			Select:      publicExposureSelectModel{Kind: types.StringValue("globs"), Biomes: null, Allow: strList("/users/*"), Deny: null},
			CorsOrigins: null,
		},
	}}
}

func TestPublicExposureSpecShape(t *testing.T) {
	spec := declaredExposure().toSpec()
	want := map[string]any{"entries": []any{
		map[string]any{
			"host": "api.example.com", "pathPrefix": "/api",
			"select":      map[string]any{"kind": "all", "deny": []any{"/billing/**"}},
			"corsOrigins": []any{"https://app.example.com"},
		},
		map[string]any{
			"host": "example.com", "pathPrefix": "/api",
			"select": map[string]any{"kind": "globs", "allow": []any{"/users/*"}},
		},
	}}
	if !reflect.DeepEqual(spec, want) {
		t.Fatalf("unexpected spec:\n got %#v\nwant %#v", spec, want)
	}
}

func TestPublicExposureReadbackRoundTrips(t *testing.T) {
	declared := declaredExposure()
	var got publicExposureModel
	err := got.applyReadback(&client.Resource{
		Spec:   declared.toSpec(),
		Status: map[string]any{"configured": true, "revision": float64(7)},
	})
	if err != nil {
		t.Fatal(err)
	}
	declared.ID = types.StringValue("installation")
	declared.Revision = types.Int64Value(7)
	declared.Configured = types.BoolValue(true)
	if !reflect.DeepEqual(got.toSpec(), declared.toSpec()) {
		t.Fatalf("spec did not round-trip: %#v", got.toSpec())
	}
	if !got.Entries[1].CorsOrigins.IsNull() || !got.Entries[0].Select.Biomes.IsNull() {
		t.Fatalf("an omitted optional list must read back null")
	}
	if got.Revision.ValueInt64() != 7 || !got.Configured.ValueBool() || got.ID.ValueString() != "installation" {
		t.Fatalf("status not applied: %#v", got)
	}
}

func TestPublicExposureReadbackRefusesMissingStatus(t *testing.T) {
	var m publicExposureModel
	err := m.applyReadback(&client.Resource{Spec: map[string]any{"entries": []any{}}})
	if err == nil {
		t.Fatal("a read-back without status must fail, not default")
	}
}

func TestPublicExposureSchema(t *testing.T) {
	var resp resource.SchemaResponse
	(&publicExposureResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	if a := resp.Schema.Attributes["entries"]; !a.IsRequired() {
		t.Errorf("entries is required")
	}
	for _, name := range []string{"id", "revision", "configured"} {
		if a := resp.Schema.Attributes[name]; !a.IsComputed() || a.IsOptional() || a.IsRequired() {
			t.Errorf("%s must be Computed only", name)
		}
	}
}

func TestPublicExposureSelectKindValidation(t *testing.T) {
	for kind, wantErr := range map[string]bool{"all": false, "biomes": false, "globs": false, "regex": true, "": true} {
		var resp validator.StringResponse
		selectKindValidator{}.ValidateString(context.Background(),
			validator.StringRequest{ConfigValue: types.StringValue(kind)}, &resp)
		if resp.Diagnostics.HasError() != wantErr {
			t.Errorf("kind %q: error=%v, want %v", kind, resp.Diagnostics.HasError(), wantErr)
		}
	}
}

func TestPublicExposureImportOnlyInstallation(t *testing.T) {
	var resp resource.ImportStateResponse
	(&publicExposureResource{}).ImportState(context.Background(), resource.ImportStateRequest{ID: "other"}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("import with an id other than installation must be refused")
	}
}
