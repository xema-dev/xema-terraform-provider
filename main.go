// Copyright 2026 xema-dev
// SPDX-License-Identifier: Apache-2.0

// Command xema-terraform-provider is the Terraform provider plugin binary for
// the Xema platform (Xema-as-Code). It binds to the Xema control-plane-api and
// exposes the wired managed-resource kinds as Terraform resources.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/xema-dev/xema-terraform-provider/internal/provider"
)

// version is set at build time via -ldflags. It defaults to "dev".
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		// Registry address used by `terraform init` to resolve this provider.
		Address: "registry.terraform.io/xema-dev/xema",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err.Error())
	}
}
