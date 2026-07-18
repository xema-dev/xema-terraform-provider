// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

var (
	_ datasource.DataSource              = (*biomeInstallDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*biomeInstallDataSource)(nil)
)

type biomeInstallDataSource struct {
	client *client.Client
}

// NewBiomeInstallDataSource is the factory registered with the provider.
func NewBiomeInstallDataSource() datasource.DataSource {
	return &biomeInstallDataSource{}
}

func (d *biomeInstallDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_biome_install"
}

func (d *biomeInstallDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing org-scoped biome install by its physical id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Biome install id (the control-plane physical id).",
			},
			"biome_id": schema.StringAttribute{
				Computed:    true,
				Description: "Biome id installed. Immutable natural key (one install per biomeId per org).",
			},
			"config_json": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Install configuration as a JSON object.",
			},
			"connections": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Bound org connections as a JSON array of { adapterKind, orgConnectionId }.",
			},
			"resources": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Bound resources as a JSON array of { adapterKind, selector }.",
			},
		},
	}
}

func (d *biomeInstallDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			"Expected *client.Client. This is a bug in the provider; please report it.")
		return
	}
	d.client = c
}

func (d *biomeInstallDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data biomeInstallModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, biomeInstallKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read biome_install", err.Error())
		return
	}
	data.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
