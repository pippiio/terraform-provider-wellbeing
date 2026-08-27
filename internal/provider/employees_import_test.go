package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

// importResourceFor builds an employees resource wired to a client scoped to
// companyID, together with the empty state Terraform hands to ImportState.
func importResourceFor(t *testing.T, companyID string) (*employeesResource, *resource.ImportStateResponse) {
	t.Helper()

	client, err := wellbeingclient.NewClient("https://example.invalid", "test-token", companyID)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var schemaResp resource.SchemaResponse
	(&employeesResource{}).Schema(context.Background(), resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("Schema: %v", schemaResp.Diagnostics)
	}

	return &employeesResource{client: client}, &resource.ImportStateResponse{
		State: tfsdk.State{
			Schema: schemaResp.Schema,
			Raw:    tftypes.NewValue(schemaResp.Schema.Type().TerraformType(context.Background()), nil),
		},
	}
}

func TestImportStateAcceptsMatchingCompanyID(t *testing.T) {
	r, resp := importResourceFor(t, "1000")

	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "1000"}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("expected import to succeed, got: %v", resp.Diagnostics)
	}

	var id types.String
	if diags := resp.State.GetAttribute(context.Background(), path.Root("id"), &id); diags.HasError() {
		t.Fatalf("reading id from state: %v", diags)
	}
	if id.ValueString() != "1000" {
		t.Fatalf("id = %q, want %q", id.ValueString(), "1000")
	}
}

func TestImportStateRejectsMismatchedCompanyID(t *testing.T) {
	// Importing with another company's ID must not seed state: the client is
	// scoped to a single company, so a Read would silently return the roster of
	// the configured company under the imported ID.
	for _, importID := range []string{"2000", "", "10000", " 1000", "1000 "} {
		t.Run("id="+importID, func(t *testing.T) {
			r, resp := importResourceFor(t, "1000")

			r.ImportState(context.Background(), resource.ImportStateRequest{ID: importID}, resp)

			if !resp.Diagnostics.HasError() {
				t.Fatalf("expected an error importing %q against company 1000", importID)
			}
			if got := resp.Diagnostics.Errors()[0].Summary(); got != "Invalid Import ID" {
				t.Fatalf("summary = %q, want %q", got, "Invalid Import ID")
			}
			if detail := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(detail, "company ID") {
				t.Fatalf("detail = %q, want it to mention the company ID", detail)
			}

			// Nothing was written to state, so Terraform does not adopt the resource.
			if !resp.State.Raw.IsNull() {
				t.Fatalf("state was populated despite the company ID mismatch: %v", resp.State.Raw)
			}
		})
	}
}

func TestImportStateMatchesTrimmedProviderCompanyID(t *testing.T) {
	// The provider trims the configured company_id, so an import ID that matches
	// the trimmed value is the one that must be accepted.
	r, resp := importResourceFor(t, "  1000  ")

	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "1000"}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("expected import to succeed against a trimmed company id, got: %v", resp.Diagnostics)
	}

	var id attr.Value
	if diags := resp.State.GetAttribute(context.Background(), path.Root("id"), &id); diags.HasError() {
		t.Fatalf("reading id from state: %v", diags)
	}
}
