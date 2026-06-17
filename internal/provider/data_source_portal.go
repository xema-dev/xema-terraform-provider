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
	_ datasource.DataSource              = (*portalDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*portalDataSource)(nil)
)

type portalDataSource struct {
	client *client.Client
}

// NewPortalDataSource is the factory registered with the provider.
func NewPortalDataSource() datasource.DataSource {
	return &portalDataSource{}
}

func (d *portalDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_portal"
}

func (d *portalDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing Xema portal (org-scoped App) by its physical id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Portal id (the control-plane physical id).",
			},
			"slug": schema.StringAttribute{
				Computed:    true,
				Description: "Portal slug. Immutable natural key.",
			},
			"display_name": schema.StringAttribute{
				Computed:    true,
				Description: "Human-readable portal name.",
			},
			"default_zone": schema.StringAttribute{
				Computed:    true,
				Description: "Default zone for the portal.",
			},
			"branding": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Portal branding as a JSON object.",
			},
			"lockfile": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Distribution lockfile as a JSON object.",
			},
			"installed_biomes": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Installed biomes as a JSON array.",
			},
			"capability_policy": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Capability policy as a JSON array.",
			},
			"default_audience": schema.StringAttribute{
				Computed:    true,
				Description: "Optional default audience for the portal.",
			},
			"subdomain": schema.StringAttribute{
				Computed:    true,
				Description: "Optional portal subdomain.",
			},
			"subdomain_enabled": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the subdomain is enabled.",
			},
			"access_grants": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Optional access grants as a JSON array.",
			},
			"archived": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the portal is archived.",
			},
		},
	}
}

func (d *portalDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *portalDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data portalModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, portalKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read portal", err.Error())
		return
	}
	data.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
