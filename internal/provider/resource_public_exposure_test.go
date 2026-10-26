// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
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

func TestPublicExposureIsOrgScoped(t *testing.T) {
	cfgResp := configureProvider(t, "http://unused.invalid", noOrg)
	if cfgResp.Diagnostics.HasError() {
		t.Fatalf("operator mode must configure without org: %v", cfgResp.Diagnostics)
	}
	ctx := context.Background()

	rResp := &resource.ConfigureResponse{}
	NewPublicExposureResource().(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: cfgResp.ResourceData}, rResp)
	requireScopeDiagnostic(t, "xema_public_exposure", rResp.Diagnostics, true)

	dResp := &datasource.ConfigureResponse{}
	NewPublicExposureDataSource().(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{ProviderData: cfgResp.DataSourceData}, dResp)
	requireScopeDiagnostic(t, "data.xema_public_exposure", dResp.Diagnostics, true)
}

func TestPublicExposureRequestCarriesTheOrgHeader(t *testing.T) {
	var calls []recordedCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, recordedCall{r.Method, r.URL.Path, r.Header.Get("X-Xema-Org-Id"), r.Header.Get("Authorization")})
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(client.Handle{Kind: publicExposureKind, PhysicalID: publicExposureID, ManagedKey: publicExposureID})
			return
		}
		_ = json.NewEncoder(w).Encode(client.Resource{
			Kind: publicExposureKind, PhysicalID: publicExposureID,
			Spec:   map[string]any{"entries": []any{}},
			Status: map[string]any{"configured": true, "revision": 1},
		})
	}))
	t.Cleanup(srv.Close)

	cfgResp := configureProvider(t, srv.URL, someOrg)
	if cfgResp.Diagnostics.HasError() {
		t.Fatalf("org mode must configure: %v", cfgResp.Diagnostics)
	}
	ctx := context.Background()
	r := &publicExposureResource{}
	r.Configure(ctx, resource.ConfigureRequest{ProviderData: cfgResp.ResourceData}, &resource.ConfigureResponse{})
	plan := publicExposureModel{Entries: []publicExposureEntryModel{}}
	var diags testDiags
	if !r.write(ctx, &plan, &diags) {
		t.Fatalf("write failed: %v", diags)
	}
	if len(calls) != 2 || calls[0].method != http.MethodPost || calls[0].path != "/control-plane/resources/public-exposure" ||
		calls[1].method != http.MethodGet || calls[1].path != "/control-plane/resources/public-exposure/installation" {
		t.Fatalf("unexpected calls: %+v", calls)
	}
	for _, c := range calls {
		if c.org != "org-123" || c.auth != "Bearer tok" {
			t.Errorf("call must carry the org header and token: %+v", c)
		}
	}
}

// testDiags collects errors from write, which takes any AddError sink.
type testDiags []string

func (d *testDiags) AddError(summary, detail string) { *d = append(*d, summary+": "+detail) }
