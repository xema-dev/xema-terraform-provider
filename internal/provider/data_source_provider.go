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
	_ datasource.DataSource              = (*providerDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*providerDataSource)(nil)
)

type providerDataSource struct {
	client *client.Client
}

// NewProviderDataSource is the factory registered with the provider.
func NewProviderDataSource() datasource.DataSource {
	return &providerDataSource{}
}

func (d *providerDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_provider"
}

func (d *providerDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing LLM provider by its physical id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Service-minted provider id (the control-plane physical id).",
			},
			"name": schema.StringAttribute{
				Computed:    true,
				Description: "Human-readable provider name.",
			},
			"slug": schema.StringAttribute{
				Computed:    true,
				Description: "URL-safe provider slug (unique within the org; the managed key).",
			},
			"api_type": schema.StringAttribute{
				Computed:    true,
				Description: "Provider API protocol type (e.g. openai, anthropic).",
			},
			"base_url": schema.StringAttribute{
				Computed:    true,
				Description: "Provider API base URL.",
			},
			"auth_type": schema.StringAttribute{
				Computed:    true,
				Description: "Provider authentication type.",
			},
			"api_key": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Provider API key. Write-only: the service never returns it, so it reads null.",
			},
			"max_concurrent_requests": schema.Int64Attribute{
				Computed:    true,
				Description: "Maximum concurrent requests permitted against this provider.",
			},
			"is_active": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the provider is active.",
			},
		},
	}
}

func (d *providerDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *providerDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data providerSpecModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, providerKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read provider", err.Error())
		return
	}
	data.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
