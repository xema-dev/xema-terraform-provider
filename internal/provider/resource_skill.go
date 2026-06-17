// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// skillKind is the XemaResourceKind wire value for skills.
const skillKind = "skill"

var (
	_ resource.Resource                = (*skillResource)(nil)
	_ resource.ResourceWithConfigure   = (*skillResource)(nil)
	_ resource.ResourceWithImportState = (*skillResource)(nil)
)

type skillResource struct {
	client *client.Client
}

// NewSkillResource is the factory registered with the provider.
func NewSkillResource() resource.Resource {
	return &skillResource{}
}

// skillModel mirrors the skill kind spec plus the server-owned physical id
// stored in state. The slug is the immutable natural key.
type skillModel struct {
	ID            types.String `tfsdk:"id"`
	Slug          types.String `tfsdk:"slug"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	Scope         types.String `tfsdk:"scope"`
	Kind          types.String `tfsdk:"kind"`
	InjectionMode types.String `tfsdk:"injection_mode"`
	Category      types.String `tfsdk:"category"`
	ParentSlug    types.String `tfsdk:"parent_slug"`
	Version       types.String `tfsdk:"version"`
	SkillMarkdown types.String `tfsdk:"skill_markdown"`
	Tags          types.List   `tfsdk:"tags"`
}

func (r *skillResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_skill"
}

func (r *skillResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Xema Skill (folder bundle), managed through skill-registry-api via the control plane.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Service-minted skill id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"slug": schema.StringAttribute{
				Required:    true,
				Description: "Skill slug (hierarchical path). Immutable natural key.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable skill name (SKILL.md frontmatter name).",
			},
			"description": schema.StringAttribute{
				Required:    true,
				Description: "Skill description (SKILL.md frontmatter description).",
			},
			"scope": schema.StringAttribute{
				Required:    true,
				Description: "SkillScope enum value (e.g. system, biome, org, project, user).",
			},
			"kind": schema.StringAttribute{
				Required:    true,
				Description: "SkillSourceKind enum value (e.g. biome, authored, git_repo).",
			},
			"injection_mode": schema.StringAttribute{
				Required:    true,
				Description: "Skill injection mode.",
			},
			"category": schema.StringAttribute{
				Optional:    true,
				Description: "Free-form skill category.",
			},
			"parent_slug": schema.StringAttribute{
				Optional:    true,
				Description: "Parent skill slug for hierarchical (sub-)skills.",
			},
			"version": schema.StringAttribute{
				Optional:    true,
				Description: "Skill version.",
			},
			"skill_markdown": schema.StringAttribute{
				Optional:    true,
				Description: "SKILL.md content body.",
			},
			"tags": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Free-form tags.",
			},
		},
	}
}

func (r *skillResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, &resp.Diagnostics)
}

func (m skillModel) toSpec(ctx context.Context) (map[string]any, error) {
	spec := map[string]any{
		"slug":          m.Slug.ValueString(),
		"name":          m.Name.ValueString(),
		"description":   m.Description.ValueString(),
		"scope":         m.Scope.ValueString(),
		"kind":          m.Kind.ValueString(),
		"injectionMode": m.InjectionMode.ValueString(),
	}
	if v := optString(m.Category); v != "" {
		spec["category"] = v
	}
	if v := optString(m.ParentSlug); v != "" {
		spec["parentSlug"] = v
	}
	if v := optString(m.Version); v != "" {
		spec["version"] = v
	}
	if v := optString(m.SkillMarkdown); v != "" {
		spec["skillMarkdown"] = v
	}

	if !m.Tags.IsNull() && !m.Tags.IsUnknown() {
		var tags []string
		if diags := m.Tags.ElementsAs(ctx, &tags, false); diags.HasError() {
			return nil, errFromDiags(diags)
		}
		if len(tags) > 0 {
			spec["tags"] = tags
		}
	}
	return spec, nil
}

func (m *skillModel) applyReadback(ctx context.Context, spec map[string]any) error {
	m.Slug = types.StringValue(specString(spec, "slug"))
	m.Name = types.StringValue(specString(spec, "name"))
	m.Description = types.StringValue(specString(spec, "description"))
	m.Scope = types.StringValue(specString(spec, "scope"))
	m.Kind = types.StringValue(specString(spec, "kind"))
	m.InjectionMode = types.StringValue(specString(spec, "injectionMode"))
	m.Category = strOrNull(specString(spec, "category"))
	m.ParentSlug = strOrNull(specString(spec, "parentSlug"))
	m.Version = strOrNull(specString(spec, "version"))
	m.SkillMarkdown = strOrNull(specString(spec, "skillMarkdown"))

	if tags := stringList(spec, "tags"); len(tags) > 0 {
		lv, diags := types.ListValueFrom(ctx, types.StringType, tags)
		if diags.HasError() {
			return errFromDiags(diags)
		}
		m.Tags = lv
	} else {
		m.Tags = types.ListNull(types.StringType)
	}
	return nil
}

func (r *skillResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan skillModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Invalid skill", err.Error())
		return
	}
	handle, err := r.client.Create(ctx, skillKind, spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create skill", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, skillKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read skill after create", err.Error())
		return
	}
	if err := plan.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map skill", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *skillResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state skillModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, skillKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read skill", err.Error())
		return
	}
	if err := state.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map skill", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *skillResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan skillModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Invalid skill", err.Error())
		return
	}
	res, err := r.client.Update(ctx, skillKind, plan.ID.ValueString(), spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update skill", err.Error())
		return
	}
	if err := plan.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map skill", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *skillResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state skillModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, skillKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete skill", err.Error())
	}
}

func (r *skillResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
