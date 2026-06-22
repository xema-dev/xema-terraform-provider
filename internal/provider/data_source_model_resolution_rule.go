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
	resp.Schema = schema.Schema{
		Description: "Look up an existing Model Resolution Matrix rule by its physical id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Service-minted rule id (the control-plane physical id).",
			},
			"selector": schema.SingleNestedAttribute{
				Computed:    true,
				Description: "Dimensional selector. An empty/absent selector is the DEFAULT rule (set is_default).",
				Attributes: map[string]schema.Attribute{
					"agent":       schema.StringAttribute{Computed: true, Description: "Match on agent slug."},
					"skill":       schema.StringAttribute{Computed: true, Description: "Match on skill slug."},
					"project":     schema.StringAttribute{Computed: true, Description: "Match on project id."},
					"stage":       schema.StringAttribute{Computed: true, Description: "Match on pipeline stage/phase key."},
					"purpose":     schema.StringAttribute{Computed: true, Description: "Match on invocation purpose."},
					"model_class": schema.StringAttribute{Computed: true, Description: "Match on the resolving agent's model class (e.g. coding, review) — the Phase-4 modelClass dimension."},
					"extra": schema.MapAttribute{
						Computed:    true,
						ElementType: types.StringType,
						Description: "Any registry-added selector dimension, as a string map.",
					},
				},
			},
			"target_kind": schema.StringAttribute{
				Computed:    true,
				Description: "How the rule resolves a model (the ModelResolutionTargetKind).",
			},
			"target_model_id": schema.StringAttribute{
				Computed:    true,
				Description: "Resolve to a specific model id.",
			},
			"target_provider_slug": schema.StringAttribute{
				Computed:    true,
				Description: "Resolve within a specific provider slug.",
			},
			"target_model_class": schema.StringAttribute{
				Computed:    true,
				Description: "Resolve by model strategy class.",
			},
			"target_temperature": schema.Float64Attribute{
				Computed:    true,
				Description: "Sampling temperature applied at resolution.",
			},
			"priority": schema.Int64Attribute{
				Computed:    true,
				Description: "Tie-breaker priority when two rules match the same number of dimensions.",
			},
			"is_default": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether this is the org DEFAULT rule (empty selector).",
			},
		},
	}
}

func (d *modelResolutionRuleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
