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

// deliverableSpecKind is the XemaResourceKind wire value for deliverable specs.
const deliverableSpecKind = "deliverable-spec"

var (
	_ resource.Resource                = (*deliverableSpecResource)(nil)
	_ resource.ResourceWithConfigure   = (*deliverableSpecResource)(nil)
	_ resource.ResourceWithImportState = (*deliverableSpecResource)(nil)
)

type deliverableSpecResource struct {
	client *client.Client
}

// NewDeliverableSpecResource is the factory registered with the provider.
func NewDeliverableSpecResource() resource.Resource {
	return &deliverableSpecResource{}
}

// deliverableSpecModel mirrors the deliverable-spec kind spec plus the
// server-owned physical id stored in state. The (slug, version) pair is the
// immutable natural key.
type deliverableSpecModel struct {
	ID              types.String `tfsdk:"id"`
	Slug            types.String `tfsdk:"slug"`
	Version         types.String `tfsdk:"version"`
	Title           types.String `tfsdk:"title"`
	Kind            types.String `tfsdk:"kind"`
	Category        types.String `tfsdk:"category"`
	Description     types.String `tfsdk:"description"`
	Complexity      types.String `tfsdk:"complexity"`
	Content         types.String `tfsdk:"content"`
	ZodSchemaSource types.String `tfsdk:"zod_schema_source"`
	VersioningMode  types.String `tfsdk:"versioning_mode"`
	MultiPage       types.Bool   `tfsdk:"multi_page"`
	Tags            types.List   `tfsdk:"tags"`
	Phases          types.List   `tfsdk:"phases"`
	WorkTypes       types.List   `tfsdk:"work_types"`
}

func (r *deliverableSpecResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_deliverable_spec"
}

func (r *deliverableSpecResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Xema deliverable spec, managed through deliverable-specs-api via the control plane. " +
			"The (slug, version) pair is the immutable natural key.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Service-minted deliverable-spec id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"slug": schema.StringAttribute{
				Required:    true,
				Description: "Deliverable-spec slug. Immutable natural key.",
			},
			"version": schema.StringAttribute{
				Required:    true,
				Description: "Deliverable-spec semver. Immutable natural key.",
			},
			"title": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable deliverable-spec title.",
			},
			"kind": schema.StringAttribute{
				Required:    true,
				Description: "DeliverableSpecKind enum value (e.g. DOCUMENT_TEMPLATE, ZOD_SCHEMA, JSON_SCHEMA, STRUCTURED_JSON, ENDPOINT_FETCH, CUSTOM).",
			},
			"category": schema.StringAttribute{
				Required:    true,
				Description: "Deliverable-spec category.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Free-form deliverable-spec description.",
			},
			"complexity": schema.StringAttribute{
				Optional:    true,
				Description: "Deliverable-spec complexity.",
			},
			"content": schema.StringAttribute{
				Optional:    true,
				Description: "Deliverable-spec content body.",
			},
			"zod_schema_source": schema.StringAttribute{
				Optional:    true,
				Description: "Zod schema source for the deliverable spec.",
			},
			"versioning_mode": schema.StringAttribute{
				Optional:    true,
				Description: "Versioning mode (append | new | replace).",
			},
			"multi_page": schema.BoolAttribute{
				Optional:    true,
				Description: "Whether the deliverable is multi-page.",
			},
			"tags": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Free-form tags.",
			},
			"phases": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Pipeline phases this spec applies to.",
			},
			"work_types": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Work types this spec applies to.",
			},
		},
	}
}

func (r *deliverableSpecResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, &resp.Diagnostics)
}

func (m deliverableSpecModel) toSpec(ctx context.Context) (map[string]any, error) {
	spec := map[string]any{
		"slug":     m.Slug.ValueString(),
		"version":  m.Version.ValueString(),
		"title":    m.Title.ValueString(),
		"kind":     m.Kind.ValueString(),
		"category": m.Category.ValueString(),
	}
	if v := optString(m.Description); v != "" {
		spec["description"] = v
	}
	if v := optString(m.Complexity); v != "" {
		spec["complexity"] = v
	}
	if v := optString(m.Content); v != "" {
		spec["content"] = v
	}
	if v := optString(m.ZodSchemaSource); v != "" {
		spec["zodSchemaSource"] = v
	}
	if v := optString(m.VersioningMode); v != "" {
		spec["versioningMode"] = v
	}
	if !m.MultiPage.IsNull() && !m.MultiPage.IsUnknown() {
		spec["multiPage"] = m.MultiPage.ValueBool()
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
	if !m.Phases.IsNull() && !m.Phases.IsUnknown() {
		var phases []string
		if diags := m.Phases.ElementsAs(ctx, &phases, false); diags.HasError() {
			return nil, errFromDiags(diags)
		}
		if len(phases) > 0 {
			spec["phases"] = phases
		}
	}
	if !m.WorkTypes.IsNull() && !m.WorkTypes.IsUnknown() {
		var workTypes []string
		if diags := m.WorkTypes.ElementsAs(ctx, &workTypes, false); diags.HasError() {
			return nil, errFromDiags(diags)
		}
		if len(workTypes) > 0 {
			spec["workTypes"] = workTypes
		}
	}
	return spec, nil
}

func (m *deliverableSpecModel) applyReadback(ctx context.Context, spec map[string]any) error {
	m.Slug = types.StringValue(specString(spec, "slug"))
	m.Version = types.StringValue(specString(spec, "version"))
	m.Title = types.StringValue(specString(spec, "title"))
	m.Kind = types.StringValue(specString(spec, "kind"))
	m.Category = types.StringValue(specString(spec, "category"))
	m.Description = strOrNull(specString(spec, "description"))
	m.Complexity = strOrNull(specString(spec, "complexity"))
	m.Content = strOrNull(specString(spec, "content"))
	m.ZodSchemaSource = strOrNull(specString(spec, "zodSchemaSource"))
	m.VersioningMode = strOrNull(specString(spec, "versioningMode"))

	if v, ok := spec["multiPage"].(bool); ok {
		m.MultiPage = types.BoolValue(v)
	} else {
		m.MultiPage = types.BoolNull()
	}

	if tags := stringList(spec, "tags"); len(tags) > 0 {
		lv, diags := types.ListValueFrom(ctx, types.StringType, tags)
		if diags.HasError() {
			return errFromDiags(diags)
		}
		m.Tags = lv
	} else {
		m.Tags = types.ListNull(types.StringType)
	}
	if phases := stringList(spec, "phases"); len(phases) > 0 {
		lv, diags := types.ListValueFrom(ctx, types.StringType, phases)
		if diags.HasError() {
			return errFromDiags(diags)
		}
		m.Phases = lv
	} else {
		m.Phases = types.ListNull(types.StringType)
	}
	if workTypes := stringList(spec, "workTypes"); len(workTypes) > 0 {
		lv, diags := types.ListValueFrom(ctx, types.StringType, workTypes)
		if diags.HasError() {
			return errFromDiags(diags)
		}
		m.WorkTypes = lv
	} else {
		m.WorkTypes = types.ListNull(types.StringType)
	}
	return nil
}

func (r *deliverableSpecResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan deliverableSpecModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Invalid deliverable spec", err.Error())
		return
	}
	handle, err := r.client.Create(ctx, deliverableSpecKind, spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create deliverable spec", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, deliverableSpecKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read deliverable spec after create", err.Error())
		return
	}
	if err := plan.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map deliverable spec", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *deliverableSpecResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state deliverableSpecModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, deliverableSpecKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read deliverable spec", err.Error())
		return
	}
	if err := state.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map deliverable spec", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *deliverableSpecResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan deliverableSpecModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Invalid deliverable spec", err.Error())
		return
	}
	res, err := r.client.Update(ctx, deliverableSpecKind, plan.ID.ValueString(), spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update deliverable spec", err.Error())
		return
	}
	if err := plan.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map deliverable spec", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *deliverableSpecResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state deliverableSpecModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, deliverableSpecKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete deliverable spec", err.Error())
	}
}

func (r *deliverableSpecResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
