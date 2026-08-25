package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"wellbeing": providerserver.NewProtocol6WithError(New("test")()),
}

func TestProviderMetadataTypeName(t *testing.T) {
	t.Parallel()

	resp := &provider.MetadataResponse{}
	New("test")().Metadata(context.Background(), provider.MetadataRequest{}, resp)

	if resp.TypeName != "wellbeing" {
		t.Errorf("TypeName = %q, want %q", resp.TypeName, "wellbeing")
	}
	if resp.Version != "test" {
		t.Errorf("Version = %q, want %q", resp.Version, "test")
	}
}

func TestProviderSchemaIsValid(t *testing.T) {
	t.Parallel()

	resp := &provider.SchemaResponse{}
	New("test")().Schema(context.Background(), provider.SchemaRequest{}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("provider schema has errors: %v", resp.Diagnostics)
	}
	for _, name := range []string{"host", "token", "company_id"} {
		if _, ok := resp.Schema.Attributes[name]; !ok {
			t.Errorf("provider schema missing attribute %q", name)
		}
	}
}
