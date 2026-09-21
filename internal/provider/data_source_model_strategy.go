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
		Description: "Look up an existing model strategy by its physical id — its lane bindings and " +
			"the default model an unbound lane falls through to.",
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
			"tier": schema.StringAttribute{
				Computed:    true,
				Description: "Capacity tier.",
			},
			"provider_focus": schema.StringAttribute{
				Computed:    true,
				Description: "Provider focus.",
			},
			"is_default": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether this is the organization's default strategy.",
			},
			"default_model_slug": schema.StringAttribute{
				Computed:    true,
				Description: "The model an unbound lane routes to.",
			},
			"default_temperature": schema.Float64Attribute{
				Computed:    true,
				Description: "Temperature for the default binding.",
			},
			"default_reasoning_effort": schema.StringAttribute{
				Computed:    true,
				Description: "Reasoning effort for the default binding.",
			},
			"entries": schema.SetNestedAttribute{
				Computed:    true,
				Description: "Lane bindings, at most one per lane.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"model_lane": schema.StringAttribute{
							Computed:    true,
							Description: "The model lane this entry routes.",
						},
						"model_slug": schema.StringAttribute{
							Computed:    true,
							Description: "Model slug the lane routes to.",
						},
						"temperature": schema.Float64Attribute{
							Computed:    true,
							Description: "Temperature for this entry.",
						},
						"reasoning_effort": schema.StringAttribute{
							Computed:    true,
							Description: "Reasoning effort for this entry.",
						},
					},
				},
			},
		},
	}
}

func (d *modelStrategyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
