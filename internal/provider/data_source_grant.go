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
	_ datasource.DataSource              = (*grantDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*grantDataSource)(nil)
)

type grantDataSource struct {
	client *client.Client
}

// NewGrantDataSource is the factory registered with the provider.
func NewGrantDataSource() datasource.DataSource {
	return &grantDataSource{}
}

func (d *grantDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_grant"
}

func (d *grantDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing capability grant by its physical id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Grant id (the control-plane physical id).",
			},
			"subject_kind": schema.StringAttribute{
				Computed:    true,
				Description: "Kind of the subject the grant binds (e.g. user, service, agent).",
			},
			"subject_ref": schema.StringAttribute{
				Computed:    true,
				Description: "Reference (slug/id) of the subject the grant binds.",
			},
			"capability": schema.StringAttribute{
				Computed:    true,
				Description: "Capability ref the grant authorizes.",
			},
			"environment": schema.StringAttribute{
				Computed:    true,
				Description: "Execution environment slug the grant applies to.",
			},
			"resource_glob": schema.StringAttribute{
				Computed:    true,
				Description: "Optional resource glob the grant scopes to. Defaults server-side to \"**\".",
			},
			"requires_approval": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether invocations under this grant require approval.",
			},
			"profile": schema.StringAttribute{
				Computed:    true,
				Description: "Optional authorization profile applied to the grant.",
			},
			"constraints": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Optional constraints as a JSON object.",
			},
		},
	}
}

func (d *grantDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *grantDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data grantModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, grantKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read grant", err.Error())
		return
	}
	data.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
