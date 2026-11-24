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
	_ datasource.DataSource              = (*credentialDefaultDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*credentialDefaultDataSource)(nil)
)

type credentialDefaultDataSource struct {
	client *client.Client
}

// NewCredentialDefaultDataSource is the factory registered with the provider.
func NewCredentialDefaultDataSource() datasource.DataSource {
	return &credentialDefaultDataSource{}
}

func (d *credentialDefaultDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_credential_default"
}

func (d *credentialDefaultDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a credential default by its id (`org:<provider key>` or " +
			"`project:<project id>:<provider key>`).",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Required: true, Description: "The default's id."},
			"scope_tier": schema.StringAttribute{Computed: true, Description: "`org` or `project`."},
			"project_id": schema.StringAttribute{Computed: true, Description: "The project id, for a project default."},
			"llm_provider": schema.SingleNestedAttribute{
				Computed:    true,
				Description: "Always null: the server keeps only the derived provider key.",
				Attributes: map[string]schema.Attribute{
					"slug":     schema.StringAttribute{Computed: true, Description: "The LLM provider slug."},
					"base_url": schema.StringAttribute{Computed: true, Description: "The provider endpoint base URL."},
				},
			},
			"credential_binding_id": schema.StringAttribute{Computed: true, Description: "The default credential binding's id."},
			"provider_key":          schema.StringAttribute{Computed: true, Description: "The derived provider key."},
		},
	}
}

func (d *credentialDefaultDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (d *credentialDefaultDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data credentialDefaultModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := d.client.Read(ctx, credentialDefaultKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read credential default", err.Error())
		return
	}
	data.applyReadback(res)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
