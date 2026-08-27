package provider

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

// These tests pin both halves of a behaviour that pulls in opposite directions.
//
// The provider must not report a diff when the API merely echoes a configured
// value back in its own shape — a bare phone returned in international form, a
// single `name` returned split into Firstname and Lastname. That is what the
// carry-forward of configured spellings exists to prevent.
//
// But it must report a diff when someone edits an employee in the Wellbeing
// portal, or Terraform silently accepts the change forever and the roster stops
// being described by its configuration.
//
// Telling the two apart requires comparing what config *resolves to* against
// what the API holds, rather than trusting one side blindly.

func configuredEmployee() employeeModel {
	return employeeModel{
		ID:    types.StringValue("mj"),
		Email: types.StringValue("mj@example.dk"),
		Name:  types.StringValue("Mogens Jensen"),
		Phone: types.StringValue("12121212"),
	}
}

// apiEmployeeFor renders what the API returns for an unmodified employee: the
// name split in two and the phone normalised to international form.
func apiEmployeeFor(phone string) wellbeingclient.Employee {
	contact := phone
	return wellbeingclient.Employee{
		EmployeeID:    "mj",
		Email:         "mj@example.dk",
		Firstname:     "Mogens",
		Lastname:      "Jensen",
		ContactNumber: &contact,
	}
}

func TestFromAPIEmployeeKeepsConfiguredPhoneWhenOnlyNormalised(t *testing.T) {
	// Config says 12121212, the API says +4512121212. Same number, different
	// shape — reporting the API's form would diff on every plan.
	prior := configuredEmployee()

	got, diags := fromAPIEmployee(context.Background(), apiEmployeeFor("+4512121212"), &prior, "+45")
	if diags.HasError() {
		t.Fatalf("fromAPIEmployee: %v", diags)
	}

	if got.Phone.ValueString() != "12121212" {
		t.Errorf("phone = %q, want the configured %q — normalisation is not drift",
			got.Phone.ValueString(), "12121212")
	}
}

func TestFromAPIEmployeeReportsPhoneChangedOutsideTerraform(t *testing.T) {
	// Someone edited the phone in the Wellbeing portal. Terraform must see it.
	prior := configuredEmployee()

	got, diags := fromAPIEmployee(context.Background(), apiEmployeeFor("+4599999999"), &prior, "+45")
	if diags.HasError() {
		t.Fatalf("fromAPIEmployee: %v", diags)
	}

	if got.Phone.ValueString() == "12121212" {
		t.Fatal("drift was not detected: state kept the configured phone and the portal change was discarded")
	}
	if got.Phone.ValueString() != "+4599999999" {
		t.Errorf("phone = %q, want the value the API holds", got.Phone.ValueString())
	}
}

func TestFromAPIEmployeeReportsPhoneAddedOutsideTerraform(t *testing.T) {
	// Config sets no phone; someone added one in the portal.
	prior := configuredEmployee()
	prior.Phone = types.StringNull()

	got, _ := fromAPIEmployee(context.Background(), apiEmployeeFor("+4531313131"), &prior, "+45")

	if got.Phone.IsNull() {
		t.Error("drift was not detected: a phone added in the portal left state null")
	}
}

func TestFromAPIEmployeeReportsPhoneRemovedOutsideTerraform(t *testing.T) {
	// Config sets a phone; someone cleared it in the portal.
	prior := configuredEmployee()
	api := apiEmployeeFor("")
	api.ContactNumber = nil

	got, _ := fromAPIEmployee(context.Background(), api, &prior, "+45")

	if got.Phone.ValueString() == "12121212" {
		t.Error("drift was not detected: a phone cleared in the portal kept the configured value")
	}
}

func TestFromAPIEmployeeKeepsConfiguredNameWhenOnlySplit(t *testing.T) {
	// Config expressed one `name`; the API returns it split. Not drift.
	prior := configuredEmployee()

	got, diags := fromAPIEmployee(context.Background(), apiEmployeeFor("+4512121212"), &prior, "+45")
	if diags.HasError() {
		t.Fatalf("fromAPIEmployee: %v", diags)
	}

	if got.Name.ValueString() != "Mogens Jensen" {
		t.Errorf("name = %q, want the configured form", got.Name.ValueString())
	}
	if !got.Firstname.IsNull() || !got.Lastname.IsNull() {
		t.Errorf("firstname/lastname = %q/%q, want null — config never set them",
			got.Firstname.ValueString(), got.Lastname.ValueString())
	}
}

func TestFromAPIEmployeeReportsNameChangedOutsideTerraform(t *testing.T) {
	prior := configuredEmployee()
	api := apiEmployeeFor("+4512121212")
	api.Firstname = "Morten" // edited in the portal

	got, _ := fromAPIEmployee(context.Background(), api, &prior, "+45")

	if got.Name.ValueString() == "Mogens Jensen" && got.Firstname.IsNull() {
		t.Fatal("drift was not detected: the portal's first name was discarded")
	}
	if got.Firstname.ValueString() != "Morten" {
		t.Errorf("firstname = %q, want the value the API holds", got.Firstname.ValueString())
	}
}

func TestFromAPIEmployeeKeepsExplicitNameOverridesWhenUnchanged(t *testing.T) {
	// An employee whose name was corrected with explicit firstname/lastname
	// must not be mistaken for drift.
	prior := employeeModel{
		ID:        types.StringValue("rds"),
		Email:     types.StringValue("rds@example.dk"),
		Name:      types.StringValue("Rosario"),
		Lastname:  types.StringValue("de Silva"),
		Firstname: types.StringNull(),
	}
	api := wellbeingclient.Employee{
		EmployeeID: "rds", Email: "rds@example.dk",
		Firstname: "Rosario", Lastname: "de Silva",
	}

	got, _ := fromAPIEmployee(context.Background(), api, &prior, "+45")

	if got.Name.ValueString() != "Rosario" {
		t.Errorf("name = %q, want the configured form", got.Name.ValueString())
	}
	if got.Lastname.ValueString() != "de Silva" {
		t.Errorf("lastname = %q, want the configured override", got.Lastname.ValueString())
	}
}

func TestFromAPIEmployeeStillDetectsDriftInFieldsReadDirectly(t *testing.T) {
	// Control: fields that were never carried forward have always surfaced
	// drift. This guards against a fix that breaks them.
	prior := configuredEmployee()
	title := "Titel sat i portalen"
	api := apiEmployeeFor("+4512121212")
	api.JobTitle = &title
	api.EmploymentStatus = 1 // on leave, set outside Terraform

	got, _ := fromAPIEmployee(context.Background(), api, &prior, "+45")

	if got.JobTitle.ValueString() != title {
		t.Errorf("job_title = %q, want the portal value", got.JobTitle.ValueString())
	}
	if got.Active.ValueBool() {
		t.Error("active = true, want false — employment status was changed in the portal")
	}
}

func TestFromAPIEmployeeCannotDetectInvitationDateDrift(t *testing.T) {
	// Documents a real limitation rather than asserting a behaviour we like:
	// invitation_date is write-only, so the API never reports it and drift in
	// it is undetectable by any means.
	prior := configuredEmployee()
	prior.InvitationDate = types.StringValue("2030-01-01T00:00:00Z")

	got, _ := fromAPIEmployee(context.Background(), apiEmployeeFor("+4512121212"), &prior, "+45")

	if got.InvitationDate.ValueString() != "2030-01-01T00:00:00Z" {
		t.Errorf("invitation_date = %q, want the configured value carried forward; "+
			"the API never returns this field so there is nothing to compare against",
			got.InvitationDate.ValueString())
	}
}

func TestFromAPIEmployeeWithoutPriorIsUnaffected(t *testing.T) {
	// The data source passes no prior and must keep reporting the API verbatim.
	got, _ := fromAPIEmployee(context.Background(), apiEmployeeFor("+4512121212"), nil, "+45")

	if got.Phone.ValueString() != "+4512121212" {
		t.Errorf("phone = %q, want the API's form when there is no config to compare", got.Phone.ValueString())
	}
	if got.Firstname.ValueString() != "Mogens" {
		t.Errorf("firstname = %q, want the API's value", got.Firstname.ValueString())
	}
}

// editPhoneInPortal simulates a human changing an employee's phone number in
// the Wellbeing UI, behind Terraform's back.
func (f *fakeRoster) editPhoneInPortal(employeeID, phone string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i := range f.employees {
		if f.employees[i].EmployeeID == employeeID {
			f.employees[i].ContactNumber = &phone
			return
		}
	}
}

func TestAccEmployeesDetectsPhoneEditedInPortal(t *testing.T) {
	// End-to-end proof: without this, a change made in the portal is invisible
	// to `terraform plan` and Terraform never puts it back.
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	config := fakeProviderConfig(srv.URL) + `
resource "wellbeing_employees" "this" {
  default_country_code = "+45"

  employee {
    id    = "mj"
    name  = "Mogens Jensen"
    email = "mj@example.dk"
    phone = "12121212"
  }
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.0.phone", "12121212"),
			},
			{
				// Re-applying unchanged config must still be a no-op: the API
				// normalises the phone, and that must not read as a change.
				Config:   config,
				PlanOnly: true,
			},
			{
				// Now someone edits the phone in the portal.
				PreConfig: func() { fake.editPhoneInPortal("mj", "+4599999999") },
				Config:    config,
				// A refresh must notice, and the plan must be non-empty so the
				// next apply restores what the configuration says.
				ExpectNonEmptyPlan: true,
				PlanOnly:           true,
			},
		},
	})
}
