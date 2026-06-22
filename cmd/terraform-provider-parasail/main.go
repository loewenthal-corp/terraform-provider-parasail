package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/loewenthal-corp/terraform-provider-parasail/internal/provider"
)

var version = "dev"

func main() {
	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/loewenthal-corp/parasail",
	})
	if err != nil {
		log.Fatal(err)
	}
}
