// terraform-provider-idcloudhost is a custom, project-scoped Terraform
// provider for IDCloudHost. It is in development and not published to the
// Terraform Registry.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/hllme/terraform-provider-idcloudhost/internal/provider"
)

// version is set by the release build (goreleaser -ldflags); it stays
// "dev" for local `go build`.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/hllme/idcloudhost",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err.Error())
	}
}
