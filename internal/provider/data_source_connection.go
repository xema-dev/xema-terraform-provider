// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

var (
	_ datasource.DataSource              = (*connectionDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*connectionDataSource)(nil)
)

type connectionDataSource struct {
	client *client.Client
}

// NewConnectionDataSource is the factory registered with the provider.
func NewConnectionDataSource() datasource.DataSource {
	return &connectionDataSource{}
}

func (d *connectionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connection"
}

func (d *connectionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a connector gateway connection by id. Connections are read by id only; " +
			"no secret is ever returned.",
		Attributes: map[string]schema.Attribute{
			"id":                    schema.StringAttribute{Required: true, Description: "The connector gateway's connection id."},
			"provider_slug":         schema.StringAttribute{Computed: true, Description: "The external provider."},
			"name":                  schema.StringAttribute{Computed: true, Description: "The connection's name."},
			"scope_tier":            schema.StringAttribute{Computed: true, Description: "`org` or `user`."},
			"credential_kind":       schema.StringAttribute{Computed: true, Description: "The declared credential kind."},
			"endpoint":              schema.StringAttribute{Computed: true, Description: "The connection's endpoint."},
			"credential_binding_id": schema.StringAttribute{Computed: true, Description: "The bound credential binding id; null while absent or suspended."},
			"endpoint_revision":     schema.Int64Attribute{Computed: true, Description: "The endpoint's revision."},
			"credentials_suspended": schema.BoolAttribute{Computed: true, Description: "Whether the credentials are suspended."},
		},
	}
}

func (d *connectionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (d *connectionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data connectionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := d.client.Read(ctx, connectionKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read connection", err.Error())
		return
	}
	data.applyReadback(res)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
