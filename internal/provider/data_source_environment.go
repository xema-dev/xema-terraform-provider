// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

var (
	_ datasource.DataSource              = (*environmentDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*environmentDataSource)(nil)
)

type environmentDataSource struct {
	client *client.Client
}

// NewEnvironmentDataSource is the factory registered with the provider.
func NewEnvironmentDataSource() datasource.DataSource {
	return &environmentDataSource{}
}

func (d *environmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (d *environmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing Xema execution environment by its physical id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Environment id (the control-plane physical id).",
			},
			"slug": schema.StringAttribute{
				Computed:    true,
				Description: "Environment slug (the control-plane physical id). Immutable natural key.",
			},
			"kind": schema.StringAttribute{
				Computed:    true,
				Description: "Execution environment kind.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Free-form environment description.",
			},
			"allowed_capabilities": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Capability refs allowed within this environment.",
			},
			"approval_rules": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Optional approval rules as a JSON object or array.",
			},
			"runtime_limits": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Optional runtime limits as a JSON object.",
			},
		},
	}
}

func (d *environmentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *environmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data environmentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, environmentKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read environment", err.Error())
		return
	}
	if err := data.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map environment", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
