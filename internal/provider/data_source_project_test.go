// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

func projectDS(t *testing.T, c *client.Client) (*projectDataSource, datasource.SchemaResponse, tftypes.Object) {
	t.Helper()
	d := &projectDataSource{client: c}
	var sr datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &sr)
	if sr.Diagnostics.HasError() {
		t.Fatalf("schema: %v", sr.Diagnostics)
	}
	return d, sr, sr.Schema.Type().TerraformType(context.Background()).(tftypes.Object)
}

func projectConfig(obj tftypes.Object, id, key *string) tftypes.Value {
	mk := func(v *string) tftypes.Value {
		if v == nil {
			return tftypes.NewValue(tftypes.String, nil)
		}
		return tftypes.NewValue(tftypes.String, *v)
	}
	return tftypes.NewValue(obj, map[string]tftypes.Value{
		"id": mk(id), "managed_key": mk(key),
		"name":        tftypes.NewValue(tftypes.String, nil),
		"description": tftypes.NewValue(tftypes.String, nil),
	})
}

func strp(s string) *string { return &s }

func readProject(t *testing.T, items []map[string]any, id, key *string) (datasource.ReadResponse, map[string]string, string) {
	t.Helper()
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/control-plane/resources/project" {
			gotQuery = r.URL.Query().Get("managedKey")
			_ = json.NewEncoder(w).Encode(items)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"physicalId": "p-9", "managedKey": "forge:gim", "spec": map[string]any{"name": "gim"},
		})
	}))
	t.Cleanup(srv.Close)
	d, sr, obj := projectDS(t, client.New(srv.URL, "org-1", "tok", srv.Client()))
	cfg := projectConfig(obj, id, key)
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(obj, nil)}}
	d.Read(context.Background(), datasource.ReadRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: cfg}}, &resp)
	var got projectDataSourceModel
	out := map[string]string{}
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Get(context.Background(), &got)...)
		out["id"], out["managed_key"], out["name"] = got.ID.ValueString(), got.ManagedKey.ValueString(), got.Name.ValueString()
	}
	return resp, out, gotQuery
}

func TestProjectDataSourceByManagedKeyFound(t *testing.T) {
	items := []map[string]any{{"physicalId": "p-1", "managedKey": "forge:gim", "spec": map[string]any{"name": "gim", "description": "d"}}}
	resp, out, q := readProject(t, items, nil, strp("forge:gim"))
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if q != "forge:gim" || out["id"] != "p-1" || out["managed_key"] != "forge:gim" || out["name"] != "gim" {
		t.Fatalf("query=%q out=%v", q, out)
	}
}

func TestProjectDataSourceByManagedKeyNotFound(t *testing.T) {
	// A filtered-out list, and a list holding only a different key, are both "none".
	for _, items := range [][]map[string]any{{}, {{"physicalId": "p-2", "managedKey": "forge:other"}}} {
		resp, _, _ := readProject(t, items, nil, strp("forge:gim"))
		if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics[0].Detail(), `no project with managed_key "forge:gim" in this org`) {
			t.Fatalf("want not-found diagnostic, got %v", resp.Diagnostics)
		}
	}
}

func TestProjectDataSourceByManagedKeyAmbiguous(t *testing.T) {
	items := []map[string]any{
		{"physicalId": "p-1", "managedKey": "forge:gim"}, {"physicalId": "p-2", "managedKey": "forge:gim"},
	}
	resp, _, _ := readProject(t, items, nil, strp("forge:gim"))
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics[0].Detail(), "2 projects match") {
		t.Fatalf("want ambiguity diagnostic, got %v", resp.Diagnostics)
	}
}

func TestProjectDataSourceByIDPopulatesManagedKey(t *testing.T) {
	resp, out, _ := readProject(t, nil, strp("p-9"), nil)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if out["id"] != "p-9" || out["managed_key"] != "forge:gim" || out["name"] != "gim" {
		t.Fatalf("out=%v", out)
	}
}

func TestProjectDataSourceExactlyOneOfIDAndManagedKey(t *testing.T) {
	d, sr, obj := projectDS(t, nil)
	cases := []struct {
		name    string
		id, key *string
		wantErr bool
	}{
		{"id only", strp("p-1"), nil, false},
		{"managed_key only", nil, strp("forge:gim"), false},
		{"both", strp("p-1"), strp("forge:gim"), true},
		{"neither", nil, nil, true},
	}
	for _, tc := range cases {
		var resp datasource.ValidateConfigResponse
		d.ValidateConfig(context.Background(), datasource.ValidateConfigRequest{
			Config: tfsdk.Config{Schema: sr.Schema, Raw: projectConfig(obj, tc.id, tc.key)},
		}, &resp)
		if resp.Diagnostics.HasError() != tc.wantErr {
			t.Errorf("%s: error=%v, want %v (%v)", tc.name, resp.Diagnostics.HasError(), tc.wantErr, resp.Diagnostics)
		}
	}
}
