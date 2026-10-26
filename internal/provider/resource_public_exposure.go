// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

var (
	_ resource.Resource                = (*publicExposureResource)(nil)
	_ resource.ResourceWithConfigure   = (*publicExposureResource)(nil)
	_ resource.ResourceWithImportState = (*publicExposureResource)(nil)
)

type publicExposureResource struct {
	client *client.Client
}

// NewPublicExposureResource is the factory registered with the provider.
func NewPublicExposureResource() resource.Resource {
	return &publicExposureResource{}
}

// publicExposureModel mirrors the public-exposure kind: biome-host's document
// (`entries`) plus the read-only status the control plane returns with it.
type publicExposureModel struct {
	ID         types.String               `tfsdk:"id"`
	Entries    []publicExposureEntryModel `tfsdk:"entries"`
	Revision   types.Int64                `tfsdk:"revision"`
	Configured types.Bool                 `tfsdk:"configured"`
}

func (r *publicExposureResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_public_exposure"
}

func stringListAttribute(description string) schema.ListAttribute {
	return schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: description}
}

func (r *publicExposureResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The installation's public exposure: which hosts and path prefixes expose which public " +
			"operations, and with which CORS origins. A platform setting, one per installation (id " +
			"`installation`), written only by the installation owner (anyone else is refused with 403). " +
			"The declared `entries` replace the whole document; an empty list exposes nothing, and an " +
			"unchanged document writes nothing. Destroying this resource only stops Terraform managing " +
			"the setting: the installation's public exposure is left exactly as last written, never " +
			"reset. To change it, apply another document. Import with the id `installation`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Always `installation`: the installation's single public-exposure setting.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"entries": schema.ListNestedAttribute{
				Required: true,
				Description: "The exposure entries (at most 64). Order is kept. The document is validated " +
					"whole by biome-host, which names every problem on refusal.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"host": schema.StringAttribute{
							Required:    true,
							Description: "The request host this entry governs (lower-case DNS name, optional port).",
						},
						"path_prefix": schema.StringAttribute{
							Required: true,
							Description: "Path prefix beginning with `/`, no trailing slash; the next segment " +
								"is the service name.",
						},
						"select": schema.SingleNestedAttribute{
							Required:    true,
							Description: "Which public operations the entry publishes.",
							Attributes: map[string]schema.Attribute{
								"kind": schema.StringAttribute{
									Required:    true,
									Description: "`all`, `biomes` or `globs`.",
									Validators:  []validator.String{selectKindValidator{}},
								},
								"biomes": stringListAttribute("Installed biome ids; required for `biomes`, refused for any other kind."),
								"allow":  stringListAttribute("Route globs `/<service>/<template>`; required for `globs`, refused for any other kind."),
								"deny":   stringListAttribute("Route globs removed from the selection; allowed with any kind, and wins over every selection."),
							},
						},
						"cors_origins": stringListAttribute("Absent: the entry's own origin plus registration origins. Present: exactly this list."),
					},
				},
			},
			"revision": schema.Int64Attribute{
				Computed:    true,
				Description: "Read-only: the setting's revision, advanced by each write.",
			},
			"configured": schema.BoolAttribute{
				Computed:    true,
				Description: "Read-only: whether the setting has ever been written.",
			},
		},
	}
}

func (r *publicExposureResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

func (m publicExposureModel) toSpec() map[string]any {
	entries := make([]any, 0, len(m.Entries))
	for _, e := range m.Entries {
		entries = append(entries, entryToSpec(e))
	}
	return map[string]any{"entries": entries}
}

func (m *publicExposureModel) applyReadback(res *client.Resource) error {
	raw, ok := res.Spec["entries"].([]any)
	if !ok {
		return fmt.Errorf("public-exposure read-back has no entries list")
	}
	m.ID = types.StringValue(publicExposureID)
	m.Entries = make([]publicExposureEntryModel, 0, len(raw))
	for _, e := range raw {
		m.Entries = append(m.Entries, entryFromSpec(e))
	}
	revision, ok := res.Status["revision"].(float64)
	if !ok {
		return fmt.Errorf("public-exposure read-back has no status.revision")
	}
	configured, ok := res.Status["configured"].(bool)
	if !ok {
		return fmt.Errorf("public-exposure read-back has no status.configured")
	}
	m.Revision = types.Int64Value(int64(revision))
	m.Configured = types.BoolValue(configured)
	return nil
}

// write sends the declared document (create and update are the same replace
// upstream) and stores the read-back, so state is what the setting holds.
func (r *publicExposureResource) write(ctx context.Context, plan *publicExposureModel, diags interface {
	AddError(summary, detail string)
}) bool {
	handle, err := r.client.Create(ctx, publicExposureKind, plan.toSpec())
	if err != nil {
		diags.AddError("Failed to write public exposure", err.Error())
		return false
	}
	res, err := r.client.Read(ctx, publicExposureKind, handle.PhysicalID)
	if err != nil {
		diags.AddError("Failed to read public exposure after write", err.Error())
		return false
	}
	if err := plan.applyReadback(res); err != nil {
		diags.AddError("Unexpected public exposure read-back", err.Error())
		return false
	}
	return true
}

func (r *publicExposureResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan publicExposureModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.write(ctx, &plan, &resp.Diagnostics) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

func (r *publicExposureResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state publicExposureModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, publicExposureKind, publicExposureID)
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read public exposure", err.Error())
		return
	}
	if err := state.applyReadback(res); err != nil {
		resp.Diagnostics.AddError("Unexpected public exposure read-back", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *publicExposureResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan publicExposureModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.write(ctx, &plan, &resp.Diagnostics) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

// Delete only drops the resource from state: the control plane stops managing
// the setting and calls nothing, so the installation's exposure stays as
// written. Resetting it would silently re-open the installation.
func (r *publicExposureResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("Public exposure left as written",
		"Terraform no longer manages the installation's public exposure; the setting stays exactly as "+
			"last written. Apply another document to change it.")
}

func (r *publicExposureResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != publicExposureID {
		resp.Diagnostics.AddError("Invalid import id",
			fmt.Sprintf("the installation has one public exposure; import it with id %q, got %q.", publicExposureID, req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
