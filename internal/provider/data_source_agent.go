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
	_ datasource.DataSource              = (*agentDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*agentDataSource)(nil)
)

type agentDataSource struct {
	client *client.Client
}

// NewAgentDataSource is the factory registered with the provider.
func NewAgentDataSource() datasource.DataSource {
	return &agentDataSource{}
}

func (d *agentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_agent"
}

func (d *agentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing Xema Agent (agent composition) by its physical id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Service-minted agent id (the control-plane physical id).",
			},
			"slug": schema.StringAttribute{
				Computed:    true,
				Description: "Agent slug. Immutable natural key.",
			},
			"version": schema.StringAttribute{
				Computed:    true,
				Description: "Agent semver. Immutable natural key.",
			},
			"display_name": schema.StringAttribute{
				Computed:    true,
				Description: "Human-readable agent name.",
			},
			"scope": schema.StringAttribute{
				Computed:    true,
				Description: "CompositionScope value (User | Project | Org | Biome | System).",
			},
			"root": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "The root CompositionNode as a JSON object.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Free-form agent description.",
			},
			"capability": schema.StringAttribute{
				Computed:    true,
				Description: "Optional capability ref exposed by this agent.",
			},
			"workspace": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Optional workspace configuration as a JSON object.",
			},
		},
	}
}

func (d *agentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *agentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data agentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, agentKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read agent", err.Error())
		return
	}
	data.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
