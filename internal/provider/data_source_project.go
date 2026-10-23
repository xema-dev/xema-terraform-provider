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
	_ datasource.DataSource                   = (*projectDataSource)(nil)
	_ datasource.DataSourceWithConfigure      = (*projectDataSource)(nil)
	_ datasource.DataSourceWithValidateConfig = (*projectDataSource)(nil)
)

type projectDataSource struct {
	client *client.Client
}

// NewProjectDataSource is the factory registered with the provider.
func NewProjectDataSource() datasource.DataSource {
	return &projectDataSource{}
}

// projectDataSourceModel adds the managed key, which the project resource does
// not expose, to the project fields.
type projectDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	ManagedKey  types.String `tfsdk:"managed_key"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

func (d *projectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *projectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing Xema project by its physical id or by its managed key (exactly one).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Service-minted project id. Set exactly one of `id` or `managed_key`.",
			},
			"managed_key": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Managed key of a service-managed project (`<namespace>:<id>`). Set exactly one of `id` or `managed_key`.",
			},
			"name": schema.StringAttribute{
				Computed:    true,
				Description: "Project name.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Project description.",
			},
		},
	}
}

func (d *projectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

// ValidateConfig enforces exactly one of id / managed_key at plan time. Written
// as ValidateConfig for the same reason as the provider resource's credential
// rule: this provider does not depend on terraform-plugin-framework-validators.
func (d *projectDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var cfg projectDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	hasID := !cfg.ID.IsNull()
	hasKey := !cfg.ManagedKey.IsNull()
	switch {
	case hasID && hasKey:
		resp.Diagnostics.AddError("Exactly one of id or managed_key", "`id` and `managed_key` are mutually exclusive; set only one.")
	case !hasID && !hasKey:
		resp.Diagnostics.AddError("Exactly one of id or managed_key", "Set either `id` or `managed_key` to select the project.")
	}
}

func (d *projectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data projectDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var res *client.Resource
	if !data.ManagedKey.IsNull() {
		key := data.ManagedKey.ValueString()
		items, err := d.client.ListByManagedKey(ctx, projectKind, key)
		if err != nil {
			resp.Diagnostics.AddError("Failed to look up project by managed_key", err.Error())
			return
		}
		var matches []client.Resource
		for _, it := range items {
			// A null managedKey decodes to "" and never matches.
			if it.ManagedKey != "" && it.ManagedKey == key {
				matches = append(matches, it)
			}
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddError("Project not found", fmt.Sprintf("no project with managed_key %q in this org", key))
			return
		case 1:
			res = &matches[0]
		default:
			resp.Diagnostics.AddError("Ambiguous managed_key", fmt.Sprintf("%d projects match managed_key %q in this org; expected exactly one", len(matches), key))
			return
		}
	} else {
		var err error
		res, err = d.client.Read(ctx, projectKind, data.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read project", err.Error())
			return
		}
	}
	data.ID = types.StringValue(res.PhysicalID)
	data.ManagedKey = strOrNull(res.ManagedKey)
	data.Name = types.StringValue(specString(res.Spec, "name"))
	data.Description = strOrNull(specString(res.Spec, "description"))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
