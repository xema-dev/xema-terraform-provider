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
	_ datasource.DataSource              = (*modelStrategyDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*modelStrategyDataSource)(nil)
)

type modelStrategyDataSource struct {
	client *client.Client
}

// NewModelStrategyDataSource is the factory registered with the provider.
func NewModelStrategyDataSource() datasource.DataSource {
	return &modelStrategyDataSource{}
}

func (d *modelStrategyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model_strategy"
}

func (d *modelStrategyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing model strategy (a named container of rules) by its physical id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Service-minted strategy id (the control-plane physical id).",
			},
			"slug": schema.StringAttribute{
				Computed:    true,
				Description: "Stable slug, unique within the organization.",
			},
			"display_name": schema.StringAttribute{
				Computed:    true,
				Description: "Human-readable strategy name.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "What this strategy is for.",
			},
		},
	}
}

func (d *modelStrategyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (d *modelStrategyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data modelStrategyModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, modelStrategyKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read model strategy", err.Error())
		return
	}
	data.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
