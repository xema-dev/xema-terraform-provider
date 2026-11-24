// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

var (
	_ datasource.DataSource              = (*egressDestinationsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*egressDestinationsDataSource)(nil)
)

type egressDestinationsDataSource struct {
	client *client.Client
}

// NewEgressDestinationsDataSource is the factory registered with the provider.
func NewEgressDestinationsDataSource() datasource.DataSource {
	return &egressDestinationsDataSource{}
}

func (d *egressDestinationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_egress_destinations"
}

func (d *egressDestinationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Read the installation's egress destination policy (the one policy, id `installation`). " +
			"Fails when the policy has never been configured.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Always `installation`; may be omitted.",
			},
			"kind": schema.StringAttribute{Computed: true, Description: "The policy kind."},
			"grants": schema.StringAttribute{
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "The policy's grants as a JSON list; null when it has none.",
			},
			"revision":   schema.Int64Attribute{Computed: true, Description: "The policy's revision."},
			"configured": schema.BoolAttribute{Computed: true, Description: "Whether the policy has ever been written."},
		},
	}
}

func (d *egressDestinationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (d *egressDestinationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data egressDestinationsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !data.ID.IsNull() && !data.ID.IsUnknown() && data.ID.ValueString() != egressDestinationsID {
		resp.Diagnostics.AddError("Invalid egress destinations id",
			fmt.Sprintf("the installation has one egress destination policy, id %q; got %q.", egressDestinationsID, data.ID.ValueString()))
		return
	}
	res, err := d.client.Read(ctx, egressDestinationsKind, egressDestinationsID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read egress destinations", err.Error())
		return
	}
	if err := data.applyReadback(res); err != nil {
		resp.Diagnostics.AddError("Unexpected egress destinations read-back", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
