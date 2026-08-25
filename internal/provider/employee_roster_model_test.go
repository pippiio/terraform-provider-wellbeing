package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

func stringMap(t *testing.T, in map[string]string) types.Map {
	t.Helper()

	if in == nil {
		return types.MapNull(types.StringType)
	}

	elements := make(map[string]types.String, len(in))
	for k, v := range in {
		elements[k] = types.StringValue(v)
	}

	value, diags := types.MapValueFrom(context.Background(), types.StringType, elements)
	if diags.HasError() {
		t.Fatalf("building map: %v", diags)
	}
	return value
}

func minimalModel() employeeModel {
	return employeeModel{
		Firstname:        types.StringValue("Bilbo"),
		Lastname:         types.StringValue("Baggins"),
		Email:            types.StringValue("bilbo@shire.test"),
		EmploymentStatus: types.StringValue("active"),
		Gender:           types.StringNull(),
		PhoneNumber:      types.StringNull(),
		JobTitle:         types.StringNull(),
		InvitationDate:   types.StringNull(),
		Dimensions:       types.MapNull(types.StringType),
	}
}

func TestToAPIEmployeeMapsRequiredFields(t *testing.T) {
	t.Parallel()

	got, diags := toAPIEmployee(context.Background(), "emp-1", minimalModel())
	if diags.HasError() {
		t.Fatalf("toAPIEmployee returned diagnostics: %v", diags)
	}

	if got.EmployeeID != "emp-1" {
		t.Errorf("EmployeeID = %q, want emp-1", got.EmployeeID)
	}
	if got.Firstname != "Bilbo" || got.Lastname != "Baggins" {
		t.Errorf("name = %q %q, want Bilbo Baggins", got.Firstname, got.Lastname)
	}
	if got.EmploymentStatus != 0 {
		t.Errorf("EmploymentStatus = %d, want 0 for \"active\"", got.EmploymentStatus)
	}
	if got.Gender != nil {
		t.Errorf("Gender = %v, want nil when unset", got.Gender)
	}
}

func TestToAPIEmployeeHoistsWellKnownDimensions(t *testing.T) {
	t.Parallel()

	model := minimalModel()
	model.Dimensions = stringMap(t, map[string]string{
		"Department":       "Adventuring",
		"Role":             "1",
		"ImmediateManager": "Gandalf",
		"Location":         "The Shire",
	})

	got, diags := toAPIEmployee(context.Background(), "emp-1", model)
	if diags.HasError() {
		t.Fatalf("toAPIEmployee returned diagnostics: %v", diags)
	}

	// The API accepts these three as top-level PUT fields.
	if got.Department == nil || *got.Department != "Adventuring" {
		t.Errorf("Department = %v, want Adventuring hoisted to top level", got.Department)
	}
	if got.Role == nil || *got.Role != "1" {
		t.Errorf("Role = %v, want 1 hoisted to top level", got.Role)
	}
	if got.ImmediateManager == nil || *got.ImmediateManager != "Gandalf" {
		t.Errorf("ImmediateManager = %v, want Gandalf hoisted to top level", got.ImmediateManager)
	}

	// Everything else stays in Dimensions, and the hoisted keys are not duplicated.
	if got.Dimensions["Location"] != "The Shire" {
		t.Errorf("Dimensions[Location] = %q, want The Shire", got.Dimensions["Location"])
	}
	for _, key := range hoistedDimensionKeys {
		if _, ok := got.Dimensions[key]; ok {
			t.Errorf("Dimensions unexpectedly still contains hoisted key %q", key)
		}
	}
}

func TestToAPIEmployeeMapsPhoneNumberToPhonenumber(t *testing.T) {
	t.Parallel()

	model := minimalModel()
	model.PhoneNumber = types.StringValue("+4523232323")

	got, diags := toAPIEmployee(context.Background(), "emp-1", model)
	if diags.HasError() {
		t.Fatalf("toAPIEmployee returned diagnostics: %v", diags)
	}

	if got.Phonenumber == nil || *got.Phonenumber != "+4523232323" {
		t.Errorf("Phonenumber = %v, want +4523232323", got.Phonenumber)
	}
	if got.ContactNumber != nil {
		t.Error("ContactNumber set on a write payload; it is a read-only field")
	}
}

func TestToAPIEmployeeRejectsUnknownEnum(t *testing.T) {
	t.Parallel()

	model := minimalModel()
	model.EmploymentStatus = types.StringValue("retired")

	_, diags := toAPIEmployee(context.Background(), "emp-1", model)
	if !diags.HasError() {
		t.Error("toAPIEmployee accepted an undocumented employment status")
	}
}

func TestFromAPIEmployeeMergesDimensionsAndMapsContactNumber(t *testing.T) {
	t.Parallel()

	department := "Adventuring"
	contact := "+4523232323"
	gender := 1

	api := wellbeingclient.Employee{
		EmployeeID:                "emp-1",
		Firstname:                 "Bilbo",
		Lastname:                  "Baggins",
		Email:                     "bilbo@shire.test",
		EmploymentStatus:          1,
		Gender:                    &gender,
		ContactNumber:             &contact,
		Department:                &department,
		Dimensions:                map[string]string{"Location": "The Shire"},
		ID:                        254799,
		CompanyID:                 1000,
		Fullname:                  "Bilbo Baggins",
		ExternalID:                "ext-1",
		Locked:                    true,
		SourcedFromExternalSystem: true,
	}

	got, diags := fromAPIEmployee(context.Background(), api, nil)
	if diags.HasError() {
		t.Fatalf("fromAPIEmployee returned diagnostics: %v", diags)
	}

	if got.EmploymentStatus.ValueString() != "on_leave" {
		t.Errorf("EmploymentStatus = %q, want on_leave", got.EmploymentStatus.ValueString())
	}
	if got.Gender.ValueString() != "female" {
		t.Errorf("Gender = %q, want female", got.Gender.ValueString())
	}
	if got.PhoneNumber.ValueString() != "+4523232323" {
		t.Errorf("PhoneNumber = %q, want the value from ContactNumber", got.PhoneNumber.ValueString())
	}

	dimensions := got.Dimensions.Elements()
	if len(dimensions) != 2 {
		t.Fatalf("Dimensions = %v, want Location plus the hoisted Department", dimensions)
	}
	if dimensions["Department"].(types.String).ValueString() != "Adventuring" {
		t.Errorf("Dimensions[Department] = %v, want a top-level Department folded back in", dimensions["Department"])
	}

	if got.ID.ValueInt64() != 254799 {
		t.Errorf("ID = %d, want 254799", got.ID.ValueInt64())
	}
	if !got.Locked.ValueBool() {
		t.Error("Locked = false, want true")
	}
}

func TestFromAPIEmployeeCarriesInvitationDateForward(t *testing.T) {
	t.Parallel()

	// InvitationDate is write-only and applies only to new employees, so the API
	// never returns it. Without carrying the configured value forward from prior
	// state, every plan after the first would show a spurious diff.
	prior := minimalModel()
	prior.InvitationDate = types.StringValue("2026-09-01T08:00:00Z")

	api := wellbeingclient.Employee{
		EmployeeID:       "emp-1",
		Firstname:        "Bilbo",
		Lastname:         "Baggins",
		Email:            "bilbo@shire.test",
		EmploymentStatus: 0,
	}

	got, diags := fromAPIEmployee(context.Background(), api, &prior)
	if diags.HasError() {
		t.Fatalf("fromAPIEmployee returned diagnostics: %v", diags)
	}

	if got.InvitationDate.ValueString() != "2026-09-01T08:00:00Z" {
		t.Errorf("InvitationDate = %q, want it carried forward from prior state", got.InvitationDate.ValueString())
	}
}

func TestFromAPIEmployeeNullsEmptyDimensions(t *testing.T) {
	t.Parallel()

	api := wellbeingclient.Employee{EmployeeID: "emp-1", EmploymentStatus: 0}

	got, diags := fromAPIEmployee(context.Background(), api, nil)
	if diags.HasError() {
		t.Fatalf("fromAPIEmployee returned diagnostics: %v", diags)
	}

	if !got.Dimensions.IsNull() {
		t.Errorf("Dimensions = %v, want null so an unset attribute does not diff against {}", got.Dimensions)
	}
}

func TestRoundTripPreservesDimensions(t *testing.T) {
	t.Parallel()

	model := minimalModel()
	model.Dimensions = stringMap(t, map[string]string{
		"Department":       "Adventuring",
		"ImmediateManager": "Gandalf",
		"Location":         "The Shire",
	})

	api, diags := toAPIEmployee(context.Background(), "emp-1", model)
	if diags.HasError() {
		t.Fatalf("toAPIEmployee returned diagnostics: %v", diags)
	}

	// Simulate the server: GET returns every dimension nested, including the
	// three the PUT accepted as top-level fields.
	served := wellbeingclient.Employee{
		EmployeeID:       api.EmployeeID,
		Firstname:        api.Firstname,
		Lastname:         api.Lastname,
		Email:            api.Email,
		EmploymentStatus: api.EmploymentStatus,
		Dimensions: map[string]string{
			"Department":       "Adventuring",
			"ImmediateManager": "Gandalf",
			"Location":         "The Shire",
		},
	}

	got, diags := fromAPIEmployee(context.Background(), served, &model)
	if diags.HasError() {
		t.Fatalf("fromAPIEmployee returned diagnostics: %v", diags)
	}

	if !got.Dimensions.Equal(model.Dimensions) {
		t.Errorf("round trip changed dimensions: got %v, want %v", got.Dimensions, model.Dimensions)
	}
}

func TestBatchLimitExceeded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current int
		desired int
		limit   int
		want    bool
	}{
		{name: "under 100 employees bypasses the rule entirely", current: 99, desired: 1, limit: 25, want: false},
		{name: "exactly at the limit is rejected", current: 100, desired: 75, limit: 25, want: true},
		{name: "just under the limit is allowed", current: 100, desired: 76, limit: 25, want: false},
		{name: "growth is measured the same as shrinkage", current: 100, desired: 130, limit: 25, want: true},
		{name: "no change is always allowed", current: 1000, desired: 1000, limit: 1, want: false},
		{name: "emptying a large roster is rejected", current: 500, desired: 0, limit: 25, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := batchLimitExceeded(tt.current, tt.desired, tt.limit); got != tt.want {
				t.Errorf("batchLimitExceeded(%d, %d, %d) = %v, want %v",
					tt.current, tt.desired, tt.limit, got, tt.want)
			}
		})
	}
}
