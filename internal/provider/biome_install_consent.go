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
	case reason == consentReasonUnavailable:
		return " The consent door did not answer; apply again later."
	case consentMovedReasons[reason]:
		return " The consent moved or was settled while it was decided; apply again to plan the install afresh, " +
			"and review the grant set it shows."
	}
	return ""
}

// biomeInstallConsentRefused explains a 422 consent refusal from its closed
// reason. Only the code and the reason are quoted, never the request.
func biomeInstallConsentRefused(apiErr *client.APIError, accept types.Bool) (string, string) {
	if apiErr.Reason == consentReasonAfterRun {
		// The install ran, and the consent its run asked for afterwards was not
		// given. Accepting permissions would not change that.
		return "Biome install consent refused after the install ran", fmt.Sprintf("The control plane ran "+
			"the install, but the grant consent it asked for after the run was not given (%s, stop reason: %s). "+
			"The install may exist without that consent, and Terraform does not record it. Decide the pending "+
			"consent with a credential allowed to decide consent in this org, then import the install by its id "+
			"(or remove it) and apply again.", apiErr.Code, apiErr.Reason)
	}
	reason := apiErr.Reason
	if reason == "" {
		reason = "not reported"
	}
	detail := fmt.Sprintf("The control plane planned the install but did not give the grant consent the plan "+
		"asks for (%s, stop reason: %s); nothing ran.", apiErr.Code, reason)
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
// only that status and code.
func biomeInstallConsentRefusalUnmapped(apiErr *client.APIError) (string, string) {
	return "Biome install consent refusal unmapped", fmt.Sprintf("Control-plane received an unmapped consent "+
		"refusal (%s: %s); nothing ran. Report it.", apiErr.Code, apiErr.Message)
}
