// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

var (
	_ datasource.DataSource              = (*modelDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*modelDataSource)(nil)
)

type modelDataSource struct {
	client *client.Client
}

// NewModelDataSource is the factory registered with the provider.
func NewModelDataSource() datasource.DataSource {
	return &modelDataSource{}
}

func (d *modelDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model"
}

func (d *modelDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing LLM model (provider+model pair) by its physical id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Service-minted model id (the control-plane physical id).",
			},
			"provider_id": schema.StringAttribute{
				Computed:    true,
				Description: "LLM provider id.",
			},
			"model_id": schema.StringAttribute{
				Computed:    true,
				Description: "Provider-specific model id.",
			},
			"display_name": schema.StringAttribute{
				Computed:    true,
				Description: "Human-readable model display name.",
			},
			"context_window": schema.Int64Attribute{
				Computed:    true,
				Description: "Model context window size, in tokens.",
			},
			"capabilities": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Model capabilities.",
			},
			"modalities": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Model modalities.",
			},
			"is_active": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the model is active.",
			},
		},
	}
}

func (d *modelDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *modelDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data modelModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, modelKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read model", err.Error())
		return
	}
	if err := data.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map model", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
