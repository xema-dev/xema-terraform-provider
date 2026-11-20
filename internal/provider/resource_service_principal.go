// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"crypto/rand"
	"fmt"
	"regexp"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// servicePrincipalKind is the XemaResourceKind wire value for service principals.
const servicePrincipalKind = "service-principal"

const (
	// secretNotDeliveredCode is the typed error the service answers when a
	// create or rotation is a replay, or otherwise holds no secret to hand out.
	secretNotDeliveredCode = "SERVICE_PRINCIPAL_SECRET_NOT_DELIVERED"

	defaultRotationOverlapSeconds int64 = 300
	minRotationOverlapSeconds     int64 = 30
	maxRotationOverlapSeconds     int64 = 86400
)

var servicePrincipalNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

var (
	_ resource.Resource                = (*servicePrincipalResource)(nil)
	_ resource.ResourceWithConfigure   = (*servicePrincipalResource)(nil)
	_ resource.ResourceWithImportState = (*servicePrincipalResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*servicePrincipalResource)(nil)
)

type servicePrincipalResource struct {
	client *client.Client
}

// NewServicePrincipalResource is the factory registered with the provider.
func NewServicePrincipalResource() resource.Resource {
	return &servicePrincipalResource{}
}

// servicePrincipalModel mirrors the service-principal kind spec { name,
// displayName, description } plus the credential the service reports.
type servicePrincipalModel struct {
	ID                     types.String `tfsdk:"id"`
	Name                   types.String `tfsdk:"name"`
	DisplayName            types.String `tfsdk:"display_name"`
	Description            types.String `tfsdk:"description"`
	RotationTrigger        types.String `tfsdk:"rotation_trigger"`
	RotationOverlapSeconds types.Int64  `tfsdk:"rotation_overlap_seconds"`
	ClientID               types.String `tfsdk:"client_id"`
	TokenEndpoint          types.String `tfsdk:"token_endpoint"`
	CredentialRevision     types.String `tfsdk:"credential_revision"`
	State                  types.String `tfsdk:"state"`
	ClientSecret           types.String `tfsdk:"client_secret"`
}

// servicePrincipalNameValidator refuses a name outside ^[a-z][a-z0-9-]{0,39}$.
type servicePrincipalNameValidator struct{}

func (servicePrincipalNameValidator) Description(context.Context) string {
	return "must match " + servicePrincipalNamePattern.String()
}

func (v servicePrincipalNameValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (servicePrincipalNameValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if !servicePrincipalNamePattern.MatchString(req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid service principal name",
			fmt.Sprintf("The name must match %s; got %q.", servicePrincipalNamePattern.String(), req.ConfigValue.ValueString()))
	}
}

// nonEmptyDescriptionValidator refuses description = "": null or omitted is how
// the description is left out, and the service reads an empty one back as none.
type nonEmptyDescriptionValidator struct{}

func (nonEmptyDescriptionValidator) Description(context.Context) string {
	return "must not be empty; omit the attribute instead"
}

func (v nonEmptyDescriptionValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (nonEmptyDescriptionValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if req.ConfigValue.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Empty description",
			"The description must not be empty; omit the attribute instead.")
	}
}

// rotationOverlapValidator bounds the overlap window to what the service accepts.
type rotationOverlapValidator struct{}

func (rotationOverlapValidator) Description(context.Context) string {
	return fmt.Sprintf("must be between %d and %d", minRotationOverlapSeconds, maxRotationOverlapSeconds)
}

func (v rotationOverlapValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (rotationOverlapValidator) ValidateInt64(_ context.Context, req validator.Int64Request, resp *validator.Int64Response) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueInt64()
	if got < minRotationOverlapSeconds || got > maxRotationOverlapSeconds {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid rotation overlap",
			fmt.Sprintf("The overlap must be between %d and %d seconds; got %d.", minRotationOverlapSeconds, maxRotationOverlapSeconds, got))
	}
}

func (r *servicePrincipalResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_principal"
}

func (r *servicePrincipalResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "A service principal: a machine identity with an OAuth client (client id and secret) that a " +
			"pipeline uses to call Xema. Name, display name and description cannot change in place; any change " +
			"replaces the principal (and its credential). Change `rotation_trigger` to issue a new client secret " +
			"in place. " +
			"**The client secret is returned by the service once, on create and on each rotation, and it IS " +
			"stored in Terraform state as a sensitive value. The state backend is therefore a secret store: " +
			"keep it encrypted and access-restricted.** Write the secret straight into the CI secret store " +
			"(for example a `github_actions_secret` or a Key Vault secret resource) and do not expose it in an " +
			"output. A pipeline's principal should be given roles that confer no authority: assigning roles " +
			"needs a person's token, never a service's (see `xema_role_assignment`). " +
			"Import with the principal id; an imported principal has a null `client_secret`, so rotate " +
			"(change `rotation_trigger`) to obtain one. Destroying the principal revokes its credentials.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The principal id (the control-plane physical id).",
				PlanModifiers: keep,
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Unique name of the principal in the org: lowercase letters, digits and `-`, starting with a letter, at most 40 characters.",
				PlanModifiers: replace,
				Validators:    []validator.String{servicePrincipalNameValidator{}},
			},
			"display_name": schema.StringAttribute{
				Required:      true,
				Description:   "Human-readable name.",
				PlanModifiers: replace,
			},
			"description": schema.StringAttribute{
				Optional:      true,
				Description:   "What the principal is for. Omit it rather than set it empty.",
				PlanModifiers: replace,
				Validators:    []validator.String{nonEmptyDescriptionValidator{}},
			},
			"rotation_trigger": schema.StringAttribute{
				Optional: true,
				Description: "Any change to this value rotates the client secret in place: a new secret is issued " +
					"and the old one stays valid for `rotation_overlap_seconds`. Use a value you bump " +
					"deliberately (a date, a counter).",
			},
			"rotation_overlap_seconds": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(defaultRotationOverlapSeconds),
				Description: "How long the previous secret stays valid after a rotation, 30 to 86400 seconds " +
					"(default 300). Changing it alone does not rotate.",
				Validators: []validator.Int64{rotationOverlapValidator{}},
			},
			"client_id": schema.StringAttribute{
				Computed:    true,
				Description: "The OAuth client id.",
			},
			"token_endpoint": schema.StringAttribute{
				Computed:    true,
				Description: "The OAuth token endpoint the client id and secret authenticate against. Null after import until the first rotation.",
			},
			"credential_revision": schema.StringAttribute{
				Computed:    true,
				Description: "The revision of the current credential; it advances on each rotation.",
			},
			"state": schema.StringAttribute{
				Computed:      true,
				Description:   "Lifecycle state reported by the service (for example `ACTIVE`).",
				PlanModifiers: keep,
			},
			"client_secret": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
				Description: "The OAuth client secret, set only from the create or rotation response; a refresh " +
					"never changes it. It is stored in state (see the resource description) and is null after import.",
			},
		},
	}
}

func (r *servicePrincipalResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, orgScoped, &resp.Diagnostics)
}

// ModifyPlan keeps the credential attributes at their state values on every
// plan except one that changes rotation_trigger, where the rotation will issue
// new ones, so they are unknown. (UseStateForUnknown cannot do this: it skips a
// null state, which would plan a perpetual update after an import.) A create
// leaves them unknown and a replacement is re-planned by Terraform as a create.
func (r *servicePrincipalResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state servicePrincipalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rotating := !plan.RotationTrigger.Equal(state.RotationTrigger)
	set := func(attr string, prior types.String) {
		if rotating {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(attr), types.StringUnknown())...)
			return
		}
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(attr), prior)...)
	}
	set("client_secret", state.ClientSecret)
	set("client_id", state.ClientID)
	// The read reports no token endpoint, so an imported principal holds none:
	// keep that null, but let a rotation (which answers one) fill it.
	if rotating && state.TokenEndpoint.IsNull() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("token_endpoint"), types.StringUnknown())...)
	} else {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("token_endpoint"), state.TokenEndpoint)...)
	}
	set("credential_revision", state.CredentialRevision)
}

func (m servicePrincipalModel) toSpec() map[string]any {
	spec := map[string]any{
		"name":        m.Name.ValueString(),
		"displayName": m.DisplayName.ValueString(),
	}
	if s := optString(m.Description); s != "" {
		spec["description"] = s
	}
	return spec
}

// statusString reads a status field the service reports as a string or a JSON
// number (credentialRevision is a number there); a number becomes its integer string.
func statusString(status map[string]any, key string) string {
	switch v := status[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

// applyReadback copies the live spec and status onto m. It never touches
// client_secret: the service never reports one.
func (m *servicePrincipalModel) applyReadback(res *client.Resource) {
	m.Name = types.StringValue(specString(res.Spec, "name"))
	m.DisplayName = types.StringValue(specString(res.Spec, "displayName"))
	m.Description = strOrNull(specString(res.Spec, "description"))
	if v := statusString(res.Status, "clientId"); v != "" {
		m.ClientID = types.StringValue(v)
	}
	if v := statusString(res.Status, "tokenEndpoint"); v != "" {
		m.TokenEndpoint = types.StringValue(v)
	}
	if v := statusString(res.Status, "credentialRevision"); v != "" {
		m.CredentialRevision = types.StringValue(v)
	}
	if v := statusString(res.Status, "state"); v != "" {
		m.State = types.StringValue(v)
	}
}

// servicePrincipalGone reports a principal whose credentials are revoked or
// being revoked: it no longer exists for Terraform's purposes.
func servicePrincipalGone(res *client.Resource) bool {
	switch statusString(res.Status, "state") {
	case "REVOKED", "REVOKING":
		return true
	}
	return false
}

// newIdempotencyKey returns a random UUID (v4) for one rotation request.
func newIdempotencyKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func (r *servicePrincipalResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan servicePrincipalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	handle, err := r.client.Create(ctx, servicePrincipalKind, plan.toSpec())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create service principal", err.Error())
		return
	}
	secret := handle.OneTimeOutputs["clientSecret"]
	if secret == "" {
		// The principal exists but Terraform would hold no secret for it, and
		// the service will not hand it out again. Withdraw it so a retry can
		// create it afresh, rather than leave an untracked principal behind.
		const summary = "Service principal create returned no client secret"
		cause := "The service did not return a client secret for the new principal (" + secretNotDeliveredCode + "). "
		if delErr := r.client.Delete(ctx, servicePrincipalKind, handle.PhysicalID); delErr != nil && !client.IsNotFound(delErr) {
			resp.Diagnostics.AddError(summary, cause+"Removing the principal again failed ("+delErr.Error()+"): "+
				"principal "+handle.PhysicalID+" was NOT removed and is not tracked by Terraform. "+
				"Revoke it by hand before retrying.")
			return
		}
		resp.Diagnostics.AddError(summary, cause+"The principal was removed again; retry the apply.")
		return
	}

	plan.ID = types.StringValue(handle.PhysicalID)
	plan.ClientSecret = types.StringValue(secret)
	plan.ClientID = types.StringValue(handle.OneTimeOutputs["clientId"])
	plan.TokenEndpoint = types.StringValue(handle.OneTimeOutputs["tokenEndpoint"])
	plan.CredentialRevision = types.StringValue(handle.OneTimeOutputs["credentialRevision"])
	plan.State = types.StringNull()

	res, err := r.client.Read(ctx, servicePrincipalKind, handle.PhysicalID)
	if err != nil {
		// Keep the one-time secret: save state, tainted by the error.
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		resp.Diagnostics.AddError("Failed to read service principal after create", err.Error())
		return
	}
	plan.applyReadback(res)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *servicePrincipalResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state servicePrincipalModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Read(ctx, servicePrincipalKind, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read service principal", err.Error())
		return
	}
	if servicePrincipalGone(res) {
		resp.State.RemoveResource(ctx)
		return
	}
	state.applyReadback(res)
	if state.RotationOverlapSeconds.IsNull() {
		// An imported principal: take the default the schema would plan.
		state.RotationOverlapSeconds = types.Int64Value(defaultRotationOverlapSeconds)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update rotates the credential in place when rotation_trigger changed; every
// other attribute replaces the resource, so a change to the overlap window
// alone is only recorded.
func (r *servicePrincipalResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state servicePrincipalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.RotationTrigger.Equal(state.RotationTrigger) {
		plan.ClientSecret = state.ClientSecret
		plan.ClientID = state.ClientID
		plan.CredentialRevision = state.CredentialRevision
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	key, err := newIdempotencyKey()
	if err != nil {
		resp.Diagnostics.AddError("Failed to rotate service principal credential", err.Error())
		return
	}
	rotated, err := r.client.RotateServicePrincipalCredential(ctx, state.ID.ValueString(),
		plan.RotationOverlapSeconds.ValueInt64(), key)
	if err != nil {
		resp.Diagnostics.AddError("Failed to rotate service principal credential", err.Error())
		return
	}
	if rotated.ClientSecret == "" {
		resp.Diagnostics.AddError("Credential rotation returned no client secret",
			"The service did not return a client secret ("+secretNotDeliveredCode+"). "+
				"The credential is unchanged and rotation_trigger in state keeps its old value, so running apply again retries the rotation.")
		return
	}

	plan.ClientSecret = types.StringValue(rotated.ClientSecret)
	plan.ClientID = types.StringValue(rotated.ClientID)
	if rotated.ClientID == "" {
		plan.ClientID = state.ClientID
	}
	plan.CredentialRevision = types.StringValue(strconv.FormatInt(rotated.CredentialRevision, 10))
	if plan.TokenEndpoint.IsUnknown() {
		plan.TokenEndpoint = strOrNull(rotated.TokenEndpoint)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete revokes the principal; one that is already gone is done.
func (r *servicePrincipalResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state servicePrincipalModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, servicePrincipalKind, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete service principal", err.Error())
	}
}

func (r *servicePrincipalResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
