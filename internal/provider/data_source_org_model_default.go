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
	_ datasource.DataSource              = (*orgModelDefaultDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*orgModelDefaultDataSource)(nil)
)

type orgModelDefaultDataSource struct {
	client *client.Client
}

// NewOrgModelDefaultDataSource is the factory registered with the provider.
func NewOrgModelDefaultDataSource() datasource.DataSource {
	return &orgModelDefaultDataSource{}
}

func (d *orgModelDefaultDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_model_default"
}

func (d *orgModelDefaultDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up the organization default model by its physical id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Control-plane physical id of the organization default.",
			},
			"model_id": schema.StringAttribute{
				Computed:    true,
				Description: "Registry model record id (`LlmModel.id`) the organization routes to by default.",
			},
		},
	}
}

func (d *orgModelDefaultDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (d *orgModelDefaultDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data orgModelDefaultModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, orgModelDefaultKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read org model default", err.Error())
		return
	}
	data.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
