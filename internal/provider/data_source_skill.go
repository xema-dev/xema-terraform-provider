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
	_ datasource.DataSource              = (*skillDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*skillDataSource)(nil)
)

type skillDataSource struct {
	client *client.Client
}

// NewSkillDataSource is the factory registered with the provider.
func NewSkillDataSource() datasource.DataSource {
	return &skillDataSource{}
}

func (d *skillDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_skill"
}

func (d *skillDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing Xema Skill by its physical id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Service-minted skill id (the control-plane physical id).",
			},
			"slug": schema.StringAttribute{
				Computed:    true,
				Description: "Skill slug (hierarchical path). Immutable natural key.",
			},
			"name": schema.StringAttribute{
				Computed:    true,
				Description: "Human-readable skill name (SKILL.md frontmatter name).",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Skill description (SKILL.md frontmatter description).",
			},
			"scope": schema.StringAttribute{
				Computed:    true,
				Description: "SkillScope enum value (e.g. system, biome, org, project, user).",
			},
			"kind": schema.StringAttribute{
				Computed:    true,
				Description: "SkillSourceKind enum value (e.g. biome, authored, git_repo).",
			},
			"injection_mode": schema.StringAttribute{
				Computed:    true,
				Description: "Skill injection mode.",
			},
			"category": schema.StringAttribute{
				Computed:    true,
				Description: "Free-form skill category.",
			},
			"parent_slug": schema.StringAttribute{
				Computed:    true,
				Description: "Parent skill slug for hierarchical (sub-)skills.",
			},
			"version": schema.StringAttribute{
				Computed:    true,
				Description: "Skill version.",
			},
			"skill_markdown": schema.StringAttribute{
				Computed:    true,
				Description: "SKILL.md content body.",
			},
			"tags": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Free-form tags.",
			},
		},
	}
}

func (d *skillDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *skillDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data skillModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Read(ctx, skillKind, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read skill", err.Error())
		return
	}
	if err := data.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map skill", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
