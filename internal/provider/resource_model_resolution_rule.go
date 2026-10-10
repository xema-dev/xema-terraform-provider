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

// modelResolutionRuleKind is the XemaResourceKind wire value for Model
// Resolution Matrix rules.
const modelResolutionRuleKind = "model-resolution-rule"

// The model-lane attribute description, declared ONCE because the resource and
// the data source must describe the same wire field and the data source reuses
// this file's mrrModel — two copies could drift into describing one field two
// ways.
//
// It deliberately names NO lane as a platform value. The closed
// `ModelClass` enum these attributes were built against is deleted: every lane
// is now declared by a `model-lane` contribution, so the admissible set is a
// property of what an organisation has installed and the only honest answer is
// the endpoint that can enumerate it. The four an ordinary deployment sees come
// from the `agent-runtime` biome and are named as its contributions, not as a
// kernel core.
const (
	modelLaneSelectorDescription = "Match on the resolving agent's model lane — the `modelLane` dimension, " +
		"so one rule routes every agent on that lane. OPEN vocabulary with no kernel core: each lane is " +
		"declared by a `model-lane` contribution, and `GET /work-kinds?kind=model-lane` enumerates the lanes " +
		"this organisation may use (the `agent-runtime` biome contributes `general-purpose`, `coder`, " +
		"`planner` and `light`). An undeclared lane is refused when the rule is written, not at resolve time."
)

var (
	_ resource.Resource                = (*modelResolutionRuleResource)(nil)
	_ resource.ResourceWithConfigure   = (*modelResolutionRuleResource)(nil)
	_ resource.ResourceWithImportState = (*modelResolutionRuleResource)(nil)
)

type modelResolutionRuleResource struct {
	client *client.Client
}

// NewModelResolutionRuleResource is the factory registered with the provider.
func NewModelResolutionRuleResource() resource.Resource {
	return &modelResolutionRuleResource{}
}

// selectorModel mirrors the dimensional ModelMatrixSelector. The v1 dimensions
// (agent/skill/project/stage/purpose) are typed; `extra` carries any
// registry-added dimension as a string map.
type selectorModel struct {
	Agent     types.String `tfsdk:"agent"`
	Skill     types.String `tfsdk:"skill"`
	Project   types.String `tfsdk:"project"`
	Stage     types.String `tfsdk:"stage"`
	Purpose   types.String `tfsdk:"purpose"`
	ModelLane types.String `tfsdk:"model_lane"`
	Extra     types.Map    `tfsdk:"extra"`
}

// mrrModel mirrors the model-resolution-rule kind spec.
type mrrModel struct {
	ID                    types.String   `tfsdk:"id"`
	Selector              *selectorModel `tfsdk:"selector"`
	StrategyID            types.String   `tfsdk:"strategy_id"`
	TargetModelID         types.String   `tfsdk:"target_model_id"`
	TargetTemperature     types.Float64  `tfsdk:"target_temperature"`
	TargetReasoningEffort types.String   `tfsdk:"target_reasoning_effort"`
	Priority              types.Int64    `tfsdk:"priority"`
}

func (r *modelResolutionRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model_resolution_rule"
}

func (r *modelResolutionRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Model Resolution Matrix rule, managed through llm-registry-api via the control plane. " +
			"Among matching rules the one matching the most selector dimensions wins; ties break by priority, " +
			"then a strategy rule over an org-level rule, then the lower id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Service-minted rule id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"strategy_id": schema.StringAttribute{
				Optional: true,
				Description: "The model strategy that owns this rule; omit for an org-level rule. A strategy's " +
					"rules apply to the projects the strategy is bound to. Immutable: changing it replaces the rule.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"selector": schema.SingleNestedAttribute{
				Required:    true,
				Description: "Dimensional selector. At least one dimension is required: an empty selector is refused. The organization default model is `xema_org_model_default`.",
				Attributes: map[string]schema.Attribute{
					"agent":      schema.StringAttribute{Optional: true, Description: "Match on agent slug."},
					"skill":      schema.StringAttribute{Optional: true, Description: "Match on skill slug."},
					"project":    schema.StringAttribute{Optional: true, Description: "Match on project id."},
					"stage":      schema.StringAttribute{Optional: true, Description: "Match on pipeline stage/phase key."},
					"purpose":    schema.StringAttribute{Optional: true, Description: "Match on invocation purpose."},
					"model_lane": schema.StringAttribute{Optional: true, Description: modelLaneSelectorDescription},
					"extra": schema.MapAttribute{
						Optional:    true,
						ElementType: types.StringType,
						Description: "Any registry-added selector dimension, as a string map.",
					},
				},
			},
			"target_model_id": schema.StringAttribute{
				Required: true,
				Description: "Registry model record id (`LlmModel.id`) the rule routes to. It must be an active " +
					"model on an active provider the organization or the platform owns; otherwise the write is refused.",
			},
			"target_temperature": schema.Float64Attribute{
				Optional:    true,
				Description: "Sampling temperature (0-2) carried by the resolved model.",
			},
			"target_reasoning_effort": schema.StringAttribute{
				Optional: true,
				Description: "Reasoning effort carried by the resolved model, forwarded verbatim to the provider. " +
					"OPEN vocabulary owned by the provider.",
			},
			"priority": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Tie-breaker priority when two rules match the same number of dimensions.",
			},
		},
	}
}

func (r *modelResolutionRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (m mrrModel) toSpec(ctx context.Context) (map[string]any, error) {
	spec := map[string]any{"targetModelId": m.TargetModelID.ValueString()}

	if m.Selector != nil {
		sel := map[string]any{}
		if v := optString(m.Selector.Agent); v != "" {
			sel["agent"] = v
		}
		if v := optString(m.Selector.Skill); v != "" {
			sel["skill"] = v
		}
		if v := optString(m.Selector.Project); v != "" {
			sel["project"] = v
		}
		if v := optString(m.Selector.Stage); v != "" {
			sel["stage"] = v
		}
		if v := optString(m.Selector.Purpose); v != "" {
			sel["purpose"] = v
		}
		if v := optString(m.Selector.ModelLane); v != "" {
			sel["modelLane"] = v
		}
		if !m.Selector.Extra.IsNull() && !m.Selector.Extra.IsUnknown() {
			extra := map[string]string{}
			if diags := m.Selector.Extra.ElementsAs(ctx, &extra, false); diags.HasError() {
				return nil, errFromDiags(diags)
			}
			if len(extra) > 0 {
				sel["extra"] = extra
			}
		}
		spec["selector"] = sel
	}

	if v := optString(m.StrategyID); v != "" {
		spec["strategyId"] = v
	}
	if v := optString(m.TargetReasoningEffort); v != "" {
		spec["targetReasoningEffort"] = v
	}
	if !m.TargetTemperature.IsNull() && !m.TargetTemperature.IsUnknown() {
		spec["targetTemperature"] = m.TargetTemperature.ValueFloat64()
	}
	if !m.Priority.IsNull() && !m.Priority.IsUnknown() {
		spec["priority"] = m.Priority.ValueInt64()
	}
	return spec, nil
}

func (m *mrrModel) applyReadback(ctx context.Context, spec map[string]any) error {
	m.StrategyID = strOrNull(specString(spec, "strategyId"))
	m.TargetModelID = types.StringValue(specString(spec, "targetModelId"))
	m.TargetReasoningEffort = strOrNull(specString(spec, "targetReasoningEffort"))

	if v, ok := numberFromSpec(spec, "targetTemperature"); ok {
		m.TargetTemperature = types.Float64Value(v)
	} else {
		m.TargetTemperature = types.Float64Null()
	}
	if v, ok := numberFromSpec(spec, "priority"); ok {
		m.Priority = types.Int64Value(int64(v))
	} else {
		m.Priority = types.Int64Null()
	}

	if rawSel, ok := spec["selector"].(map[string]any); ok && len(rawSel) > 0 {
		sel := &selectorModel{
			Agent:     strOrNull(specString(rawSel, "agent")),
			Skill:     strOrNull(specString(rawSel, "skill")),
			Project:   strOrNull(specString(rawSel, "project")),
			Stage:     strOrNull(specString(rawSel, "stage")),
			Purpose:   strOrNull(specString(rawSel, "purpose")),
			ModelLane: strOrNull(specString(rawSel, "modelLane")),
			Extra:     types.MapNull(types.StringType),
		}
		if rawExtra, ok := rawSel["extra"].(map[string]any); ok && len(rawExtra) > 0 {
			elems := map[string]string{}
			for k, v := range rawExtra {
				if s, ok := v.(string); ok {
					elems[k] = s
				}
			}
			mv, diags := types.MapValueFrom(ctx, types.StringType, elems)
			if diags.HasError() {
				return errFromDiags(diags)
			}
			sel.Extra = mv
		}
		m.Selector = sel
	} else {
		m.Selector = nil
	}
	return nil
}

func (r *modelResolutionRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mrrModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Invalid selector", err.Error())
		return
	}
	handle, err := r.client.Create(ctx, modelResolutionRuleKind, spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create model resolution rule", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, modelResolutionRuleKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read model resolution rule after create", err.Error())
		return
	}
	if err := plan.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map model resolution rule", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelResolutionRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mrrModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, modelResolutionRuleKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read model resolution rule", err.Error())
		return
	}
	if err := state.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map model resolution rule", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *modelResolutionRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan mrrModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Invalid selector", err.Error())
		return
	}
	res, err := r.client.Update(ctx, modelResolutionRuleKind, plan.ID.ValueString(), spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update model resolution rule", err.Error())
		return
	}
	if err := plan.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map model resolution rule", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelResolutionRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mrrModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, modelResolutionRuleKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete model resolution rule", err.Error())
	}
}

func (r *modelResolutionRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
