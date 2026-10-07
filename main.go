package main

import (
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6/tf6server"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/provider"
)

var version = "dev"

func main() {
	debug := flag.Bool("debug", false, "run with support for debuggers like delve")
	flag.Parse()

	var opts []tf6server.ServeOpt
	if *debug {
		opts = append(opts, tf6server.WithManagedDebug())
	}
	err := tf6server.Serve("registry.terraform.io/alexmchughdev/terradash", func() tfprotov6.ProviderServer {
		return provider.New(version)
	}, opts...)
	if err != nil {
		log.Fatal(err)
	}
}
