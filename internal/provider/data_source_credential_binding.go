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
	_ datasource.DataSource              = (*credentialBindingDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*credentialBindingDataSource)(nil)
)

type credentialBindingDataSource struct {
	client *client.Client
}

// NewCredentialBindingDataSource is the factory registered with the provider.
func NewCredentialBindingDataSource() datasource.DataSource {
	return &credentialBindingDataSource{}
}

func (d *credentialBindingDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_credential_binding"
}

func (d *credentialBindingDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	list := func(desc string) schema.ListAttribute {
		return schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: desc}
	}
	resp.Schema = schema.Schema{
		Description: "Look up a live credential binding by the broker's binding id. The secret is never " +
			"returned. A revoked binding reads as absent.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Required: true, Description: "The credential broker's binding id."},
			"scope_tier":  schema.StringAttribute{Computed: true, Description: "`org` or `project`."},
			"project_id":  schema.StringAttribute{Computed: true, Description: "The project id, for a project binding."},
			"auth_type":   schema.StringAttribute{Computed: true, Description: "`bearer` or `api_key`."},
			"header_name": schema.StringAttribute{Computed: true, Description: "The header carrying the key, for `api_key`."},
			"llm_provider": schema.SingleNestedAttribute{
				Computed:    true,
				Description: "Always null: the server keeps only the derived provider key.",
				Attributes: map[string]schema.Attribute{
					"slug":     schema.StringAttribute{Computed: true, Description: "The LLM provider slug."},
					"base_url": schema.StringAttribute{Computed: true, Description: "The provider endpoint base URL."},
				},
			},
			"allowed_capabilities": list("Capabilities the binding may be used for."),
			"allowed_actions":      list("Actions the binding may be used for."),
			"allowed_resources":    list("Resources the binding may be used on."),
			"secret_value":         schema.StringAttribute{Computed: true, Sensitive: true, Description: "Always null: the secret is never returned."},
			"secret_version":       schema.StringAttribute{Computed: true, Description: "Always null: config-only on the resource."},
			"provider_key":         schema.StringAttribute{Computed: true, Description: "The derived provider key."},
			"version":              schema.Int64Attribute{Computed: true, Description: "The binding's version."},
		},
	}
}

func (d *credentialBindingDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (d *credentialBindingDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data credentialBindingModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := d.client.Read(ctx, credentialBindingKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read credential binding", err.Error())
		return
	}
	data.applyReadback(res)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
