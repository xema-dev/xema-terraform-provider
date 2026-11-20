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
	_ datasource.DataSource              = (*roleAssignmentDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*roleAssignmentDataSource)(nil)
)

type roleAssignmentDataSource struct {
	client *client.Client
}

// NewRoleAssignmentDataSource is the factory registered with the provider.
func NewRoleAssignmentDataSource() datasource.DataSource {
	return &roleAssignmentDataSource{}
}

func (d *roleAssignmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_assignment"
}

func (d *roleAssignmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing role assignment by its id, `<role_id>:<subject_kind>:<subject_ref>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "`<role_id>:<subject_kind>:<subject_ref>` (the control-plane physical id).",
			},
			"role_id": schema.StringAttribute{
				Computed:    true,
				Description: "Id of the assigned role.",
			},
			"subject_kind": schema.StringAttribute{
				Computed:    true,
				Description: "Kind of subject that holds the role.",
			},
			"subject_ref": schema.StringAttribute{
				Computed:    true,
				Description: "The subject's id.",
			},
		},
	}
}

func (d *roleAssignmentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (d *roleAssignmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data roleAssignmentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, roleAssignmentKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read role assignment", err.Error())
		return
	}
	data.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
