// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

var (
	_ datasource.DataSource              = (*publicExposureDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*publicExposureDataSource)(nil)
)

type publicExposureDataSource struct {
	client *client.Client
}

// NewPublicExposureDataSource is the factory registered with the provider.
func NewPublicExposureDataSource() datasource.DataSource {
	return &publicExposureDataSource{}
}

func (d *publicExposureDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_public_exposure"
}

func computedStringList(description string) schema.ListAttribute {
	return schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: description}
}

func (d *publicExposureDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Read the installation's public exposure (the one setting, id `installation`). " +
			"An optional list the document omits reads back null, and an empty one reads back empty.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Always `installation`; may be omitted.",
			},
			"entries": schema.ListNestedAttribute{
				Computed:    true,
				Description: "The exposure entries, in document order.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"host":        schema.StringAttribute{Computed: true, Description: "The request host the entry governs."},
						"path_prefix": schema.StringAttribute{Computed: true, Description: "Path prefix; the next segment is the service name."},
						"select": schema.SingleNestedAttribute{
							Computed:    true,
							Description: "Which public operations the entry publishes.",
							Attributes: map[string]schema.Attribute{
								"kind":   schema.StringAttribute{Computed: true, Description: "`all`, `biomes` or `globs`."},
								"biomes": computedStringList("Installed biome ids (kind `biomes`)."),
								"allow":  computedStringList("Route globs (kind `globs`)."),
								"deny":   computedStringList("Route globs removed from the selection."),
							},
						},
						"cors_origins": computedStringList("CORS origins; null when the document omits them."),
					},
				},
			},
			"revision":   schema.Int64Attribute{Computed: true, Description: "The setting's revision."},
			"configured": schema.BoolAttribute{Computed: true, Description: "Whether the setting has ever been written."},
		},
	}
}

func (d *publicExposureDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (d *publicExposureDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data publicExposureModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !data.ID.IsNull() && !data.ID.IsUnknown() && data.ID.ValueString() != publicExposureID {
		resp.Diagnostics.AddError("Invalid public exposure id",
			fmt.Sprintf("the installation has one public exposure, id %q; got %q.", publicExposureID, data.ID.ValueString()))
		return
	}

	res, err := d.client.Read(ctx, publicExposureKind, publicExposureID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read public exposure", err.Error())
		return
	}
	if err := data.applyReadback(res); err != nil {
		resp.Diagnostics.AddError("Unexpected public exposure read-back", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
