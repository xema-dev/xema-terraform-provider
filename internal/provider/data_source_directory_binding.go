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
	_ datasource.DataSource              = (*directoryBindingDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*directoryBindingDataSource)(nil)
)

type directoryBindingDataSource struct {
	client *client.Client
}

// NewDirectoryBindingDataSource is the factory registered with the provider.
func NewDirectoryBindingDataSource() datasource.DataSource {
	return &directoryBindingDataSource{}
}

func (d *directoryBindingDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_directory_binding"
}

func (d *directoryBindingDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up the organization's directory binding by org id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Organization id (the binding is one record per organization).",
			},
			"provider_ref": schema.StringAttribute{
				Computed:    true,
				Description: "The identity provider the binding trusts.",
			},
			"application_client_id": schema.StringAttribute{
				Computed:    true,
				Description: "Client id of the directory application the binding trusts.",
			},
			"member_claim": schema.SingleNestedAttribute{
				Computed:    true,
				Description: "The directory claim that marks an organization member.",
				Attributes: map[string]schema.Attribute{
					"kind": schema.StringAttribute{
						Computed:    true,
						Description: "`GROUP` or `APP_ROLE`.",
					},
					"values": schema.ListAttribute{
						Computed:    true,
						ElementType: types.StringType,
						Description: "Claim values that grant membership.",
					},
				},
			},
			"role_map": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Maps a member claim value to an org role.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"value": schema.StringAttribute{
							Computed:    true,
							Description: "A member claim value.",
						},
						"org_role": schema.StringAttribute{
							Computed:    true,
							Description: "`org_owner`, `org_admin` or `org_member`.",
						},
					},
				},
			},
			"scim_credential_id": schema.StringAttribute{
				Computed:    true,
				Description: "Uuid of the SCIM credential linked to the binding, if any.",
			},
			"authority": schema.StringAttribute{
				Computed:    true,
				Description: "Who owns membership, `claims` or `scim`.",
			},
		},
	}
}

func (d *directoryBindingDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (d *directoryBindingDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data directoryBindingModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, directoryBindingKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read directory binding", err.Error())
		return
	}
	data.applyReadback(res)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
