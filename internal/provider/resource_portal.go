// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// portalKind is the XemaResourceKind wire value for portals. A portal is an
// org-scoped App: projectId is forced null server-side and is not in the spec.
const portalKind = "portal"

var (
	_ resource.Resource                = (*portalResource)(nil)
	_ resource.ResourceWithConfigure   = (*portalResource)(nil)
	_ resource.ResourceWithImportState = (*portalResource)(nil)
)

type portalResource struct {
	client *client.Client
}

// NewPortalResource is the factory registered with the provider.
func NewPortalResource() resource.Resource {
	return &portalResource{}
}

// portalModel mirrors the portal kind spec. `slug` is the immutable natural key.
type portalModel struct {
	ID               types.String         `tfsdk:"id"`
	Slug             types.String         `tfsdk:"slug"`
	DisplayName      types.String         `tfsdk:"display_name"`
	DefaultZone      types.String         `tfsdk:"default_zone"`
	Branding         jsontypes.Normalized `tfsdk:"branding"`
	Lockfile         jsontypes.Normalized `tfsdk:"lockfile"`
	InstalledBiomes  jsontypes.Normalized `tfsdk:"installed_biomes"`
	CapabilityPolicy jsontypes.Normalized `tfsdk:"capability_policy"`
	DefaultAudience  types.String         `tfsdk:"default_audience"`
	Subdomain        types.String         `tfsdk:"subdomain"`
	SubdomainEnabled types.Bool           `tfsdk:"subdomain_enabled"`
	AccessGrants     jsontypes.Normalized `tfsdk:"access_grants"`
	Archived         types.Bool           `tfsdk:"archived"`
}

func (r *portalResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_portal"
}

func (r *portalResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Xema portal — an org-scoped App, managed through the control plane. " +
			"projectId is forced null server-side; a portal is always org-scoped.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Portal id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"slug": schema.StringAttribute{
				Required:    true,
				Description: "Portal slug. Immutable natural key.",
			},
			"display_name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable portal name.",
			},
			"default_zone": schema.StringAttribute{
				Required:    true,
				Description: "Default zone for the portal.",
			},
			"branding": schema.StringAttribute{
				Required:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Portal branding as a JSON object.",
			},
			"lockfile": schema.StringAttribute{
				Required:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Distribution lockfile as a JSON object.",
			},
			"installed_biomes": schema.StringAttribute{
				Required:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Installed biomes as a JSON array.",
			},
			"capability_policy": schema.StringAttribute{
				Required:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Capability policy as a JSON array.",
			},
			"default_audience": schema.StringAttribute{
				Optional:    true,
				Description: "Optional default audience for the portal.",
			},
			"subdomain": schema.StringAttribute{
				Optional:    true,
				Description: "Optional portal subdomain.",
			},
			"subdomain_enabled": schema.BoolAttribute{
				Optional:    true,
				Description: "Whether the subdomain is enabled.",
			},
			"access_grants": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Optional access grants as a JSON array.",
			},
			"archived": schema.BoolAttribute{
				Optional:    true,
				Description: "Whether the portal is archived.",
			},
		},
	}
}

func (r *portalResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, &resp.Diagnostics)
}

func (m portalModel) toSpec() (map[string]any, error) {
	spec := map[string]any{
		"slug":        m.Slug.ValueString(),
		"displayName": m.DisplayName.ValueString(),
		"defaultZone": m.DefaultZone.ValueString(),
	}

	branding, err := normalizedToValue(m.Branding)
	if err != nil {
		return nil, err
	}
	spec["branding"] = branding

	lockfile, err := normalizedToValue(m.Lockfile)
	if err != nil {
		return nil, err
	}
	spec["lockfile"] = lockfile

	installedBiomes, err := normalizedToValue(m.InstalledBiomes)
	if err != nil {
		return nil, err
	}
	spec["installedBiomes"] = installedBiomes

	capabilityPolicy, err := normalizedToValue(m.CapabilityPolicy)
	if err != nil {
		return nil, err
	}
	spec["capabilityPolicy"] = capabilityPolicy

	if s := optString(m.DefaultAudience); s != "" {
		spec["defaultAudience"] = s
	}
	if s := optString(m.Subdomain); s != "" {
		spec["subdomain"] = s
	}

	accessGrants, err := normalizedToValue(m.AccessGrants)
	if err != nil {
		return nil, err
	}
	if accessGrants != nil {
		spec["accessGrants"] = accessGrants
	}

	if !m.SubdomainEnabled.IsNull() && !m.SubdomainEnabled.IsUnknown() {
		spec["subdomainEnabled"] = m.SubdomainEnabled.ValueBool()
	}
	if !m.Archived.IsNull() && !m.Archived.IsUnknown() {
		spec["archived"] = m.Archived.ValueBool()
	}

	return spec, nil
}

func (m *portalModel) applyReadback(spec map[string]any) {
	m.Slug = types.StringValue(specString(spec, "slug"))
	m.DisplayName = types.StringValue(specString(spec, "displayName"))
	m.DefaultZone = types.StringValue(specString(spec, "defaultZone"))
	m.Branding = specNormalized(spec, "branding")
	m.Lockfile = specNormalized(spec, "lockfile")
	m.InstalledBiomes = specNormalized(spec, "installedBiomes")
	m.CapabilityPolicy = specNormalized(spec, "capabilityPolicy")
	m.DefaultAudience = strOrNull(specString(spec, "defaultAudience"))
	m.Subdomain = strOrNull(specString(spec, "subdomain"))
	m.AccessGrants = specNormalized(spec, "accessGrants")
	if v, ok := spec["subdomainEnabled"].(bool); ok {
		m.SubdomainEnabled = types.BoolValue(v)
	} else {
		m.SubdomainEnabled = types.BoolNull()
	}
	if v, ok := spec["archived"].(bool); ok {
		m.Archived = types.BoolValue(v)
	} else {
		m.Archived = types.BoolNull()
	}
}

func (r *portalResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan portalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec()
	if err != nil {
		resp.Diagnostics.AddError("Invalid portal spec", err.Error())
		return
	}
	handle, err := r.client.Create(ctx, portalKind, spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create portal", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, portalKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read portal after create", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *portalResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state portalModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, portalKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read portal", err.Error())
		return
	}
	state.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *portalResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan portalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec()
	if err != nil {
		resp.Diagnostics.AddError("Invalid portal spec", err.Error())
		return
	}
	res, err := r.client.Update(ctx, portalKind, plan.ID.ValueString(), spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update portal", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *portalResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state portalModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, portalKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete portal", err.Error())
	}
}

func (r *portalResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
