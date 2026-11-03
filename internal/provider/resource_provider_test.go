// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// The schema is the contract a practitioner reads. The platform refuses a
// credential reference on a provider row, so the resource must not offer one;
// `api_key` is the sole, write-only enrolment transport and must stay sensitive.
func TestProviderResourceSchemaCredentialShape(t *testing.T) {
	r := NewProviderResource()
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)

	if _, ok := resp.Schema.Attributes["credential_ref"]; ok {
		t.Error("provider resource schema must not carry `credential_ref`")
	}
	apiKey, ok := resp.Schema.Attributes["api_key"]
	if !ok {
		t.Fatal("provider resource schema is missing `api_key`")
	}
	if !apiKey.IsSensitive() {
		t.Error("`api_key` must stay sensitive")
	}
}

func TestProviderSpecOmitsCredentialRef(t *testing.T) {
	spec := providerSpecModel{}.toSpec()
	if _, ok := spec["credentialRef"]; ok {
		t.Error("request spec must not carry credentialRef")
	}
}
