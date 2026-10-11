// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

type recordedCall struct{ method, path, org, auth string }

// fakeControlPlane serves the installation org routes and records each call.
func fakeControlPlane(t *testing.T, calls *[]recordedCall) *httptest.Server {
	t.Helper()
	spec := map[string]any{"name": "acme", "displayName": "Acme"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, recordedCall{r.Method, r.URL.Path, r.Header.Get("X-Xema-Org-Id"), r.Header.Get("Authorization")})
		switch r.Method {
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.Handle{Kind: "org", PhysicalID: "org-new", ManagedKey: "acme"})
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			_ = json.NewEncoder(w).Encode(client.Resource{Kind: "org", PhysicalID: "org-new", Spec: spec})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// configureProvider runs the real provider Configure with an optional org.
func configureProvider(t *testing.T, endpoint string, org tftypes.Value) *fwprovider.ConfigureResponse {
	t.Helper()
	t.Setenv(envOrg, "")
	t.Setenv(envEndpoint, "")
	t.Setenv(envToken, "")
	p := New("test")()
	ctx := context.Background()
	var sch fwprovider.SchemaResponse
	p.Schema(ctx, fwprovider.SchemaRequest{}, &sch)
	cfg := tfsdk.Config{
		Schema: sch.Schema,
		Raw: tftypes.NewValue(sch.Schema.Type().TerraformType(ctx), map[string]tftypes.Value{
			"endpoint": tftypes.NewValue(tftypes.String, endpoint),
			"org":      org,
			"token":    tftypes.NewValue(tftypes.String, "tok"),
		}),
	}
	resp := &fwprovider.ConfigureResponse{}
	p.Configure(ctx, fwprovider.ConfigureRequest{Config: cfg}, resp)
	return resp
}

var (
	noOrg         = tftypes.NewValue(tftypes.String, nil)
	unkOrg        = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	someOrg       = tftypes.NewValue(tftypes.String, "org-123")
	orgPlanValues = map[string]tftypes.Value{
		"id":           tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"name":         tftypes.NewValue(tftypes.String, "acme"),
		"display_name": tftypes.NewValue(tftypes.String, "Acme"),
		"domain":       tftypes.NewValue(tftypes.String, nil),
		"metadata":     tftypes.NewValue(tftypes.String, nil),
	}
)

func TestOperatorModeCreatesAndReadsOrg(t *testing.T) {
	var calls []recordedCall
	srv := fakeControlPlane(t, &calls)

	cfgResp := configureProvider(t, srv.URL, noOrg)
	if cfgResp.Diagnostics.HasError() {
		t.Fatalf("operator mode must configure without org: %v", cfgResp.Diagnostics)
	}

	ctx := context.Background()
	r := NewOrgResource()
	r.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: cfgResp.ResourceData}, &resource.ConfigureResponse{})
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	plan := tfsdk.Plan{Schema: sch.Schema, Raw: tftypes.NewValue(sch.Schema.Type().TerraformType(ctx), orgPlanValues)}
	createResp := &resource.CreateResponse{State: tfsdk.State{Schema: sch.Schema, Raw: tftypes.NewValue(sch.Schema.Type().TerraformType(ctx), nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("create failed: %v", createResp.Diagnostics)
	}
	var id string
	createResp.State.GetAttribute(ctx, pathOf("id"), &id)
	if id != "org-new" {
		t.Fatalf("created org id = %q, want org-new", id)
	}

	readResp := &resource.ReadResponse{State: createResp.State}
	r.Read(ctx, resource.ReadRequest{State: createResp.State}, readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatalf("read failed: %v", readResp.Diagnostics)
	}

	if len(calls) != 3 || calls[0].method != http.MethodPost || calls[0].path != "/control-plane/installation/orgs" || calls[2].method != http.MethodGet {
		t.Fatalf("unexpected calls: %+v", calls)
	}
	for _, c := range calls {
		if c.org != "" || c.auth != "Bearer tok" {
			t.Errorf("operator call must carry the token and no org header: %+v", c)
		}
	}
}

// requireScopeDiagnostic asserts a consumer's Configure diagnostics either carry
// the org-scoped diagnostic (wantErr) or are clean.
func requireScopeDiagnostic(t *testing.T, name string, diags diag.Diagnostics, wantErr bool) {
	t.Helper()
	if !wantErr {
		if diags.HasError() {
			t.Errorf("%s must work in operator mode: %v", name, diags)
		}
		return
	}
	if !diags.HasError() {
		t.Errorf("%s must fail in operator mode", name)
		return
	}
	got := diags.Errors()[0]
	if got.Summary() != orgScopedDiagnosticSummary || !strings.Contains(got.Detail(), "this resource is org-scoped; configure the provider with org") {
		t.Errorf("%s failed with the wrong diagnostic: %s: %s", name, got.Summary(), got.Detail())
	}
}

func TestOrgScopedConsumersFailInOperatorMode(t *testing.T) {
	cfgResp := configureProvider(t, "http://unused.invalid", noOrg)
	if cfgResp.Diagnostics.HasError() {
		t.Fatalf("operator mode must configure without org: %v", cfgResp.Diagnostics)
	}
	ctx := context.Background()
	xp := New("test")().(*xemaProvider)

	for _, f := range xp.Resources(ctx) {
		r := f()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "xema"}, &meta)
		resp := &resource.ConfigureResponse{}
		r.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: cfgResp.ResourceData}, resp)
		requireScopeDiagnostic(t, "resource "+meta.TypeName, resp.Diagnostics, meta.TypeName != "xema_org")
	}
	for _, f := range xp.DataSources(ctx) {
		d := f()
		var meta datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "xema"}, &meta)
		resp := &datasource.ConfigureResponse{}
		d.(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{ProviderData: cfgResp.DataSourceData}, resp)
		requireScopeDiagnostic(t, "data source "+meta.TypeName, resp.Diagnostics, true)
	}
}

func TestOrgModeIsUnchanged(t *testing.T) {
	var calls []recordedCall
	srv := fakeControlPlane(t, &calls)
	cfgResp := configureProvider(t, srv.URL, someOrg)
	if cfgResp.Diagnostics.HasError() {
		t.Fatalf("org mode must configure: %v", cfgResp.Diagnostics)
	}
	ctx := context.Background()
	xp := New("test")().(*xemaProvider)
	for _, f := range xp.Resources(ctx) {
		resp := &resource.ConfigureResponse{}
		f().(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: cfgResp.ResourceData}, resp)
		requireScopeDiagnostic(t, "resource in org mode", resp.Diagnostics, false)
	}
	for _, f := range xp.DataSources(ctx) {
		resp := &datasource.ConfigureResponse{}
		f().(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{ProviderData: cfgResp.DataSourceData}, resp)
		requireScopeDiagnostic(t, "data source in org mode", resp.Diagnostics, false)
	}

	c := cfgResp.ResourceData.(*client.Client)
	if _, err := c.Read(ctx, "org", "org-new"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].org != "org-123" {
		t.Fatalf("org mode must send the configured org header: %+v", calls)
	}
}

func TestUnknownOrgAtPlanIsDeferredNotOperatorMode(t *testing.T) {
	cfgResp := configureProvider(t, "http://unused.invalid", unkOrg)
	if cfgResp.Diagnostics.HasError() {
		t.Fatalf("an unknown org must configure for planning: %v", cfgResp.Diagnostics)
	}
	ctx := context.Background()
	resp := &resource.ConfigureResponse{}
	NewProjectResource().(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: cfgResp.ResourceData}, resp)
	requireScopeDiagnostic(t, "org-scoped resource with a pending org", resp.Diagnostics, false)

	c := cfgResp.ResourceData.(*client.Client)
	if _, err := c.Read(ctx, "project", "p-1"); err == nil || !strings.Contains(err.Error(), "not known yet") {
		t.Fatalf("a pending-org client must refuse to call out, got %v", err)
	}
}

// The README resource table is the user-facing count of resource kinds; it must
// list exactly the resources the provider registers.
func TestReadmeResourceTableMatchesRegisteredResources(t *testing.T) {
	raw, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	rows := regexp.MustCompile("(?m)^\\| `xema_[a-z_]+`").FindAll(raw, -1)
	xp := New("test")().(*xemaProvider)
	if got, want := len(rows), len(xp.Resources(context.Background())); got != want {
		t.Fatalf("README lists %d resources, provider registers %d", got, want)
	}
}
