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

// modelKind is the XemaResourceKind wire value for models.
const modelKind = "model"

var (
	_ resource.Resource                = (*modelResource)(nil)
	_ resource.ResourceWithConfigure   = (*modelResource)(nil)
	_ resource.ResourceWithImportState = (*modelResource)(nil)
)

type modelResource struct {
	client *client.Client
}

// NewModelResource is the factory registered with the provider.
func NewModelResource() resource.Resource {
	return &modelResource{}
}

// modelModel mirrors the model kind spec (an LLM provider+model pair) plus the
// server-owned physical id stored in state.
type modelModel struct {
	ID            types.String `tfsdk:"id"`
	ProviderID    types.String `tfsdk:"provider_id"`
	ModelID       types.String `tfsdk:"model_id"`
	DisplayName   types.String `tfsdk:"display_name"`
	ContextWindow types.Int64  `tfsdk:"context_window"`
	Capabilities  types.List   `tfsdk:"capabilities"`
	Modalities    types.List   `tfsdk:"modalities"`
	IsActive      types.Bool   `tfsdk:"is_active"`
}

func (r *modelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model"
}

func (r *modelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An LLM model (provider+model pair), managed through llm-registry-api via the control plane.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Service-minted model id (the control-plane physical id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"provider_id": schema.StringAttribute{
				Required:    true,
				Description: "LLM provider id.",
			},
			"model_id": schema.StringAttribute{
				Required:    true,
				Description: "Provider-specific model id.",
			},
			"display_name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable model display name.",
			},
			"context_window": schema.Int64Attribute{
				Optional:    true,
				Description: "Model context window size, in tokens.",
			},
			"capabilities": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Model capabilities.",
			},
			"modalities": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Model modalities.",
			},
			"is_active": schema.BoolAttribute{
				Optional:    true,
				Description: "Whether the model is active.",
			},
		},
	}
}

func (r *modelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, &resp.Diagnostics)
}

func (m modelModel) toSpec(ctx context.Context) (map[string]any, error) {
	spec := map[string]any{
		"providerId":  m.ProviderID.ValueString(),
		"modelId":     m.ModelID.ValueString(),
		"displayName": m.DisplayName.ValueString(),
	}
	if !m.ContextWindow.IsNull() && !m.ContextWindow.IsUnknown() {
		spec["contextWindow"] = m.ContextWindow.ValueInt64()
	}
	if !m.IsActive.IsNull() && !m.IsActive.IsUnknown() {
		spec["isActive"] = m.IsActive.ValueBool()
	}

	if !m.Capabilities.IsNull() && !m.Capabilities.IsUnknown() {
		var capabilities []string
		if diags := m.Capabilities.ElementsAs(ctx, &capabilities, false); diags.HasError() {
			return nil, errFromDiags(diags)
		}
		if len(capabilities) > 0 {
			spec["capabilities"] = capabilities
		}
	}
	if !m.Modalities.IsNull() && !m.Modalities.IsUnknown() {
		var modalities []string
		if diags := m.Modalities.ElementsAs(ctx, &modalities, false); diags.HasError() {
			return nil, errFromDiags(diags)
		}
		if len(modalities) > 0 {
			spec["modalities"] = modalities
		}
	}
	return spec, nil
}

func (m *modelModel) applyReadback(ctx context.Context, spec map[string]any) error {
	m.ProviderID = types.StringValue(specString(spec, "providerId"))
	m.ModelID = types.StringValue(specString(spec, "modelId"))
	m.DisplayName = types.StringValue(specString(spec, "displayName"))

	if v, ok := numberFromSpec(spec, "contextWindow"); ok {
		m.ContextWindow = types.Int64Value(int64(v))
	} else {
		m.ContextWindow = types.Int64Null()
	}

	if v, ok := spec["isActive"].(bool); ok {
		m.IsActive = types.BoolValue(v)
	} else {
		m.IsActive = types.BoolNull()
	}

	if capabilities := stringList(spec, "capabilities"); len(capabilities) > 0 {
		lv, diags := types.ListValueFrom(ctx, types.StringType, capabilities)
		if diags.HasError() {
			return errFromDiags(diags)
		}
		m.Capabilities = lv
	} else {
		m.Capabilities = types.ListNull(types.StringType)
	}
	if modalities := stringList(spec, "modalities"); len(modalities) > 0 {
		lv, diags := types.ListValueFrom(ctx, types.StringType, modalities)
		if diags.HasError() {
			return errFromDiags(diags)
		}
		m.Modalities = lv
	} else {
		m.Modalities = types.ListNull(types.StringType)
	}
	return nil
}

func (r *modelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan modelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Invalid model", err.Error())
		return
	}
	handle, err := r.client.Create(ctx, modelKind, spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create model", err.Error())
		return
	}
	plan.ID = types.StringValue(handle.PhysicalID)

	res, err := r.client.Read(ctx, modelKind, handle.PhysicalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read model after create", err.Error())
		return
	}
	if err := plan.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map model", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state modelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, modelKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read model", err.Error())
		return
	}
	if err := state.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map model", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *modelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan modelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := plan.toSpec(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Invalid model", err.Error())
		return
	}
	res, err := r.client.Update(ctx, modelKind, plan.ID.ValueString(), spec)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update model", err.Error())
		return
	}
	if err := plan.applyReadback(ctx, res.Spec); err != nil {
		resp.Diagnostics.AddError("Failed to map model", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state modelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, modelKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete model", err.Error())
	}
}

func (r *modelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
