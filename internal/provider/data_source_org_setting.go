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
	_ datasource.DataSource              = (*orgSettingDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*orgSettingDataSource)(nil)
)

type orgSettingDataSource struct {
	client *client.Client
}

// NewOrgSettingDataSource is the factory registered with the provider.
func NewOrgSettingDataSource() datasource.DataSource {
	return &orgSettingDataSource{}
}

func (d *orgSettingDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_setting"
}

func (d *orgSettingDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Read the organisation's own (admin) value of one setting, by its key. " +
			"Fails when the organisation holds no value for the key.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "The setting key, `<namespace>:<name>@<major>`.",
			},
			"key": schema.StringAttribute{Computed: true, Description: "The setting key."},
			"value": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "The value in wire form, as a JSON scalar.",
			},
			"revision": schema.Int64Attribute{Computed: true, Description: "The setting's revision."},
		},
	}
}

func (d *orgSettingDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (d *orgSettingDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data orgSettingModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := d.client.Read(ctx, orgSettingKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read org setting", err.Error())
		return
	}
	data.applyReadback(res)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
