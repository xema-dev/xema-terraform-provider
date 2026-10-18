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
	_ datasource.DataSource              = (*modelResolutionRuleDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*modelResolutionRuleDataSource)(nil)
)

type modelResolutionRuleDataSource struct {
	client *client.Client
}

// NewModelResolutionRuleDataSource is the factory registered with the provider.
func NewModelResolutionRuleDataSource() datasource.DataSource {
	return &modelResolutionRuleDataSource{}
}

func (d *modelResolutionRuleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model_resolution_rule"
}

func (d *modelResolutionRuleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := ruleDataSourceAttributes()
	attrs["id"] = schema.StringAttribute{
		Required:    true,
		Description: "Service-minted rule id (the control-plane physical id).",
	}
	resp.Schema = schema.Schema{
		Description: "Look up an existing org-level Model Resolution Matrix rule by its physical id.",
		Attributes:  attrs,
	}
}

func (d *modelResolutionRuleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (d *modelResolutionRuleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data mrrModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, modelResolutionRuleKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read model resolution rule", err.Error())
		return
	}
	if err := data.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map model resolution rule", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
