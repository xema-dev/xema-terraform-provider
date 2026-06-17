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
	_ datasource.DataSource              = (*deliverableSpecDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*deliverableSpecDataSource)(nil)
)

type deliverableSpecDataSource struct {
	client *client.Client
}

// NewDeliverableSpecDataSource is the factory registered with the provider.
func NewDeliverableSpecDataSource() datasource.DataSource {
	return &deliverableSpecDataSource{}
}

func (d *deliverableSpecDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_deliverable_spec"
}

func (d *deliverableSpecDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing Xema deliverable spec by its physical id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Service-minted deliverable-spec id (the control-plane physical id).",
			},
			"slug": schema.StringAttribute{
				Computed:    true,
				Description: "Deliverable-spec slug. Immutable natural key.",
			},
			"version": schema.StringAttribute{
				Computed:    true,
				Description: "Deliverable-spec semver. Immutable natural key.",
			},
			"title": schema.StringAttribute{
				Computed:    true,
				Description: "Human-readable deliverable-spec title.",
			},
			"kind": schema.StringAttribute{
				Computed:    true,
				Description: "DeliverableSpecKind enum value (e.g. DOCUMENT_TEMPLATE, ZOD_SCHEMA, JSON_SCHEMA, STRUCTURED_JSON, ENDPOINT_FETCH, CUSTOM).",
			},
			"category": schema.StringAttribute{
				Computed:    true,
				Description: "Deliverable-spec category.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Free-form deliverable-spec description.",
			},
			"complexity": schema.StringAttribute{
				Computed:    true,
				Description: "Deliverable-spec complexity.",
			},
			"content": schema.StringAttribute{
				Computed:    true,
				Description: "Deliverable-spec content body.",
			},
			"zod_schema_source": schema.StringAttribute{
				Computed:    true,
				Description: "Zod schema source for the deliverable spec.",
			},
			"versioning_mode": schema.StringAttribute{
				Computed:    true,
				Description: "Versioning mode (append | new | replace).",
			},
			"multi_page": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the deliverable is multi-page.",
			},
			"tags": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Free-form tags.",
			},
			"phases": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Pipeline phases this spec applies to.",
			},
			"work_types": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Work types this spec applies to.",
			},
		},
	}
}

func (d *deliverableSpecDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *deliverableSpecDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data deliverableSpecModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, deliverableSpecKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read deliverable spec", err.Error())
		return
	}
	if err := data.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map deliverable spec", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
