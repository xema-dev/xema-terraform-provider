// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/xema-dev/xema-terraform-provider/internal/client"
)

// The closed `reason` values of a CONTROL_PLANE_BIOME_INSTALL_CONSENT_REFUSED
// refusal that the provider gives its own hint (control-plane-contracts
// BiomeInstallConsentRefusalReason). Any other value, including the grant
// door's and the sequence's own stops, is quoted as sent.
const (
	// consentReasonAfterRun: the install ran, then asked for a grant consent
	// that was not given.
	consentReasonAfterRun = "after-run-consent-not-given"
	// consentReasonNotAuthorized: the credential holds no role allowed to
	// decide consent in the org.
	consentReasonNotAuthorized = "not-authorized"
	// consentReasonNotAuthenticated: the credential's token is invalid or expired.
	consentReasonNotAuthenticated = "not-authenticated"
	// consentReasonUnavailable: the consent door did not answer; retry.
	consentReasonUnavailable = "unavailable"
	// consentReasonInstallationReadNotAuthorized: Biome Host refused the
	// credential reading the org's installations, before consent was asked.
	consentReasonInstallationReadNotAuthorized = "installation-read-not-authorized"

	// The consent request or what it binds moved or was settled before the
	// decision took: running the install again plans it afresh.
	consentReasonRequestNotFound        = "request-not-found"
	consentReasonConferralRevisionMoved = "conferral-revision-moved"
	consentReasonDigestChanged          = "digest-changed"
	consentReasonRequestSettled         = "request-settled"
	consentReasonSuperseded             = "superseded"
	consentReasonAdmissionMoved         = "admission-moved"
	consentReasonEligibilityExpired     = "eligibility-expired"
)

// The `stage` of a consent refusal (422 or 502): whether the install had run.
// An older control plane sends none, and then the outcome is unknown.
const (
	consentStageBeforeRun = "before-run"
	consentStageAfterRun  = "after-run"
)

// consentDecideCapability is the capability a role must hold for the
// credential to decide installation consent in the org.
const consentDecideCapability = "authorization:installation-consent.decide@1"

// consentMovedReasons are the reasons a fresh run of the install answers.
var consentMovedReasons = map[string]bool{
	consentReasonRequestNotFound:        true,
	consentReasonConferralRevisionMoved: true,
	consentReasonDigestChanged:          true,
	consentReasonRequestSettled:         true,
	consentReasonSuperseded:             true,
	consentReasonAdmissionMoved:         true,
	consentReasonEligibilityExpired:     true,
}

// consentReasonHint is the remedy a reason names on its own, or "" when the
// reason has none and is only quoted.
func consentReasonHint(reason string) string {
	switch {
	case reason == consentReasonNotAuthorized:
		return " The credential may not decide consent in this org: give it a role holding `" +
			consentDecideCapability + "`, then apply again."
	case reason == consentReasonNotAuthenticated:
		return " The credential's token is invalid or expired: renew the token the provider reads (the " +
			"`token` attribute or the " + envToken + " environment variable), then apply again."
	case reason == consentReasonInstallationReadNotAuthorized:
		return " The credential may not read the org's biome installations on Biome Host: give it a role " +
			"allowed to read them, then apply again."
	case reason == consentReasonUnavailable:
		return " The consent door did not answer; apply again later."
	case consentMovedReasons[reason]:
		return " The consent moved or was settled while it was decided; apply again to plan the install afresh, " +
			"and review the grant set it shows."
	}
	return ""
}

// consentRefusalRan reports whether the refused install ran: the server's
// stage says so, or the reason is the after-run consent itself.
func consentRefusalRan(apiErr *client.APIError) bool {
	return apiErr.Stage == consentStageAfterRun || apiErr.Reason == consentReasonAfterRun
}

// consentRefusalOutcome is the sentence on what the refusal left behind,
// from the server's typed stage alone (never its message text).
func consentRefusalOutcome(apiErr *client.APIError) string {
	switch {
	case consentRefusalRan(apiErr):
		return " The install ran: its installation exists but is unconfigured, and Terraform does not record it. " +
			"Import the install by its id (or remove it), then apply again."
	case apiErr.Stage == consentStageBeforeRun:
		return " Nothing ran."
	}
	return " This control plane does not report whether the install ran: check whether the installation exists " +
		"in the org, and if it does, import it by its id (or remove it) before applying again."
}

// biomeInstallConsentRefused explains a 422 consent refusal from its closed
// reason and stage. Only the code and the reason are quoted, never the request.
func biomeInstallConsentRefused(apiErr *client.APIError, accept types.Bool) (string, string) {
	reason := apiErr.Reason
	if reason == "" {
		reason = "not reported"
	}
	if consentRefusalRan(apiErr) {
		detail := fmt.Sprintf("The control plane ran the install, but the grant consent it asked for was not "+
			"given (%s, stop reason: %s).", apiErr.Code, reason) + consentRefusalOutcome(apiErr)
		if hint := consentReasonHint(apiErr.Reason); hint != "" {
			detail += hint
		} else {
			detail += " Decide the pending consent with a credential allowed to decide consent in this org."
		}
		return "Biome install consent refused after the install ran", detail
	}
	detail := fmt.Sprintf("The control plane did not give the grant consent the install plan asks for (%s, "+
		"stop reason: %s).", apiErr.Code, reason) + consentRefusalOutcome(apiErr)
	if hint := consentReasonHint(apiErr.Reason); hint != "" {
		return "Biome install consent refused", detail + hint
	}
	if !accept.ValueBool() {
		detail += " Set `accept_permissions = true` on this resource to consent to the grant set the plan " +
			"shows, then apply again."
	}
	return "Biome install consent refused", detail
}

// biomeInstallConsentRefusalUnmapped explains a 502: the consent door refused
// with a status and code control-plane maps to no reason. Its message names
// only that status and code; what ran follows the stage, as for the 422.
func biomeInstallConsentRefusalUnmapped(apiErr *client.APIError) (string, string) {
	return "Biome install consent refusal unmapped", fmt.Sprintf("Control-plane received an unmapped consent "+
		"refusal (%s: %s); report it.", apiErr.Code, apiErr.Message) + consentRefusalOutcome(apiErr)
}
