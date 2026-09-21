// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// modelStrategyKind is the XemaResourceKind wire value for model strategies.
const modelStrategyKind = "model-strategy"

var (
	_ resource.Resource                = (*modelStrategyResource)(nil)
	_ resource.ResourceWithConfigure   = (*modelStrategyResource)(nil)
	_ resource.ResourceWithImportState = (*modelStrategyResource)(nil)
)

type modelStrategyResource struct {
	client *client.Client
}

// NewModelStrategyResource is the factory registered with the provider.
func NewModelStrategyResource() resource.Resource {
	return &modelStrategyResource{}
}

// strategyEntryModel mirrors one lane binding: the model a given model LANE
// routes to within this strategy. A lane with no entry falls through to
// `default_model_slug`, which is the one systemic fallback that makes lane
// resolution total — no lane key is reserved or mandatory.
type strategyEntryModel struct {
	ModelLane       types.String  `tfsdk:"model_lane"`
	ModelSlug       types.String  `tfsdk:"model_slug"`
	Temperature     types.Float64 `tfsdk:"temperature"`
	ReasoningEffort types.String  `tfsdk:"reasoning_effort"`
}

type modelStrategyModel struct {
	ID                     types.String         `tfsdk:"id"`
	Slug                   types.String         `tfsdk:"slug"`
	DisplayName            types.String         `tfsdk:"display_name"`
	Description            types.String         `tfsdk:"description"`
	Tier                   types.String         `tfsdk:"tier"`
	ProviderFocus          types.String         `tfsdk:"provider_focus"`
	IsDefault              types.Bool           `tfsdk:"is_default"`
	DefaultModelSlug       types.String         `tfsdk:"default_model_slug"`
	DefaultTemperature     types.Float64        `tfsdk:"default_temperature"`
	DefaultReasoningEffort types.String         `tfsdk:"default_reasoning_effort"`
	Entries                []strategyEntryModel `tfsdk:"entries"`
}

func (r *modelStrategyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model_strategy"
}

func (r *modelStrategyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A model strategy: which model each model LANE routes to for this organization, " +
			"plus the default model an unbound lane falls through to. Managed through llm-registry-api " +
			"via the control plane.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Service-minted strategy id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"slug": schema.StringAttribute{
				Required: true,
				Description: "Stable slug, unique within the organization. It is the managed key, " +
					"and it is immutable upstream — changing it declares a different strategy.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"display_name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable strategy name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "What this strategy is for.",
			},
			"tier": schema.StringAttribute{
				Required: true,
				Description: "Capacity tier. The vocabulary is the owning service's closed set, " +
					"not a kernel enum — an unknown value is refused by llm-registry-api, not here.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"provider_focus": schema.StringAttribute{
				Required:    true,
				Description: "Provider focus. Same posture as `tier`: validated by the owning service.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"is_default": schema.BoolAttribute{
				Optional: true,
				Description: "Make this the organization's DEFAULT STRATEGY — which strategy an " +
					"invocation uses when a project binds none. A different axis from " +
					"`default_model_slug`, which is the default MODEL within a strategy.",
			},
			"default_model_slug": schema.StringAttribute{
				Required: true,
				Description: "The model an invocation routes to when the requested lane has no entry " +
					"of its own. Required: a strategy with no default cannot answer an unbound lane, " +
					"and the platform will not choose a model on an organization's behalf.",
			},
			"default_temperature": schema.Float64Attribute{
				Optional:    true,
				Description: "Temperature for the default binding (0-2). Omitted = the engine/model owns it.",
			},
			"default_reasoning_effort": schema.StringAttribute{
				Optional:    true,
				Description: "Reasoning effort for the default binding. Omitted = the engine/model owns it.",
			},
			"entries": schema.SetNestedAttribute{
				Optional: true,
				Description: "Lane bindings, at most one per lane. A SET rather than a list: the " +
					"binding for `coder` and the binding for `planner` have no order between them, " +
					"and a list would report drift whenever the service returned them in a " +
					"different order than they were written.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"model_lane": schema.StringAttribute{
							Required: true,
							Description: "The model lane this entry routes. An OPEN vocabulary: " +
								"`GET /work-kinds?kind=model-lane` enumerates what this organization " +
								"may use. An undeclared lane is refused by the owning service.",
						},
						"model_slug": schema.StringAttribute{
							Required:    true,
							Description: "Model slug, matched against the organization's registered models.",
						},
						"temperature": schema.Float64Attribute{
							Optional:    true,
							Description: "Temperature (0-2). Omitted = the engine/model owns it.",
						},
						"reasoning_effort": schema.StringAttribute{
							Optional:    true,
							Description: "Reasoning effort asked of the model. Omitted = the engine/model owns it.",
						},
					},
				},
			},
		},
	}
}

func (r *modelStrategyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, &resp.Diagnostics)
}

func (m modelStrategyModel) toSpec() map[string]any {
	spec := map[string]any{
		"slug":             m.Slug.ValueString(),
		"displayName":      m.DisplayName.ValueString(),
		"tier":             m.Tier.ValueString(),
		"providerFocus":    m.ProviderFocus.ValueString(),
		"defaultModelSlug": m.DefaultModelSlug.ValueString(),
	}
	if s := optString(m.Description); s != "" {
		spec["description"] = s
	}
	if !m.IsDefault.IsNull() && !m.IsDefault.IsUnknown() {
		spec["isDefault"] = m.IsDefault.ValueBool()
	}
	if !m.DefaultTemperature.IsNull() && !m.DefaultTemperature.IsUnknown() {
		spec["defaultTemperature"] = m.DefaultTemperature.ValueFloat64()
	}
	if s := optString(m.DefaultReasoningEffort); s != "" {
		spec["defaultReasoningEffort"] = s
	}

	// `entries` is a WHOLE-SET replace upstream, so an absent block is "say
	// nothing" and an empty one is "no lane bindings". Both are expressible and
	// they are different: emitting `[]` for an absent block would strip every
	// binding from a declaration that simply did not mention them.
	if m.Entries != nil {
		entries := make([]any, 0, len(m.Entries))
		for _, e := range m.Entries {
			entry := map[string]any{
				"modelLane": e.ModelLane.ValueString(),
				"modelSlug": e.ModelSlug.ValueString(),
			}
			if !e.Temperature.IsNull() && !e.Temperature.IsUnknown() {
				entry["temperature"] = e.Temperature.ValueFloat64()
			}
			if s := optString(e.ReasoningEffort); s != "" {
				entry["reasoningEffort"] = s
			}
			entries = append(entries, entry)
		}
		spec["entries"] = entries
	}
	return spec
}

func (m *modelStrategyModel) applyReadback(spec map[string]any) {
	m.Slug = types.StringValue(specString(spec, "slug"))
	m.DisplayName = types.StringValue(specString(spec, "displayName"))
	m.Description = strOrNull(specString(spec, "description"))
	m.Tier = types.StringValue(specString(spec, "tier"))
	m.ProviderFocus = types.StringValue(specString(spec, "providerFocus"))

	if v, ok := spec["isDefault"].(bool); ok {
		m.IsDefault = types.BoolValue(v)
	} else {
		m.IsDefault = types.BoolNull()
	}

	m.DefaultModelSlug = types.StringValue(specString(spec, "defaultModelSlug"))
	if v, ok := numberFromSpec(spec, "defaultTemperature"); ok {
		m.DefaultTemperature = types.Float64Value(v)
	} else {
		m.DefaultTemperature = types.Float64Null()
	}
	m.DefaultReasoningEffort = strOrNull(specString(spec, "defaultReasoningEffort"))

	raw, ok := spec["entries"].([]any)
	if !ok {
		m.Entries = nil
		return
	}
	entries := make([]strategyEntryModel, 0, len(raw))
	for _, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entry := strategyEntryModel{
			ModelLane:       types.StringValue(specString(obj, "modelLane")),
			ModelSlug:       types.StringValue(specString(obj, "modelSlug")),
			ReasoningEffort: strOrNull(specString(obj, "reasoningEffort")),
		}
		if v, ok := numberFromSpec(obj, "temperature"); ok {
			entry.Temperature = types.Float64Value(v)
		} else {
			entry.Temperature = types.Float64Null()
		}
		entries = append(entries, entry)
	}
	// Deterministic order. The attribute is a SET, so Terraform compares
	// order-insensitively and this is not what prevents drift — it is what makes
	// a state file and a `plan` diff readable by a person.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ModelLane.ValueString() < entries[j].ModelLane.ValueString()
	})
	m.Entries = entries
}

func (r *modelStrategyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan modelStrategyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	handle, err := r.client.Create(ctx, modelStrategyKind, plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create model strategy", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, modelStrategyKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read model strategy after create", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelStrategyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state modelStrategyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, modelStrategyKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read model strategy", err.Error())
		return
	}
	state.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *modelStrategyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan modelStrategyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Update(ctx, modelStrategyKind, plan.ID.ValueString(), plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to update model strategy", err.Error())
		return
	}
	plan.applyReadback(res.Spec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelStrategyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state modelStrategyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, modelStrategyKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete model strategy", err.Error())
	}
}

func (r *modelStrategyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
