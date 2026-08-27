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
		ID:             types.StringValue("mj@example.dk"),
		Name:           types.StringValue("Mogens Jensen"),
		Firstname:      types.StringNull(),
		Lastname:       types.StringNull(),
		Email:          types.StringValue("mj@example.dk"),
		Phone:          types.StringNull(),
		Active:         types.BoolValue(true),
		Gender:         types.StringNull(),
		JobTitle:       types.StringNull(),
		InvitationDate: types.StringNull(),
		Dimensions:     types.MapNull(types.StringType),
	}
}

func TestSplitName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		firstname string
		lastname  string
	}{
		{name: "two words", input: "Mogens Jensen", firstname: "Mogens", lastname: "Jensen"},
		{name: "compound surname stays intact", input: "Anna Van der Berg", firstname: "Anna", lastname: "Van der Berg"},
		{name: "single word leaves no surname", input: "Rosario", firstname: "Rosario", lastname: ""},
		{name: "surrounding whitespace trimmed", input: "  Mogens Glistrup  ", firstname: "Mogens", lastname: "Glistrup"},
		{name: "empty", input: "", firstname: "", lastname: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			firstname, lastname := splitName(tt.input)
			if firstname != tt.firstname || lastname != tt.lastname {
				t.Errorf("splitName(%q) = %q / %q, want %q / %q",
					tt.input, firstname, lastname, tt.firstname, tt.lastname)
			}
		})
	}
}

func TestResolveNameFromSplit(t *testing.T) {
	t.Parallel()

	firstname, lastname, diags := resolveName(minimalModel())
	if diags.HasError() {
		t.Fatalf("resolveName returned diagnostics: %v", diags)
	}
	if firstname != "Mogens" || lastname != "Jensen" {
		t.Errorf("resolveName = %q / %q, want Mogens / Jensen", firstname, lastname)
	}
}

func TestResolveNameExplicitOverridesSplit(t *testing.T) {
	t.Parallel()

	model := minimalModel()
	model.Name = types.StringValue("Anna Van der Berg")
	model.Lastname = types.StringValue("van der Berg")

	firstname, lastname, diags := resolveName(model)
	if diags.HasError() {
		t.Fatalf("resolveName returned diagnostics: %v", diags)
	}
	if firstname != "Anna" {
		t.Errorf("firstname = %q, want Anna from the split", firstname)
	}
	if lastname != "van der Berg" {
		t.Errorf("lastname = %q, want the explicit override to win", lastname)
	}
}

func TestResolveNameSingleWordNeedsExplicitLastname(t *testing.T) {
	t.Parallel()

	model := minimalModel()
	model.Name = types.StringValue("Rosario")

	if _, _, diags := resolveName(model); !diags.HasError() {
		t.Error("resolveName accepted a single-word name; the API requires a last name")
	}

	// The override rescues it.
	model.Lastname = types.StringValue("de Silva")
	firstname, lastname, diags := resolveName(model)
	if diags.HasError() {
		t.Fatalf("resolveName returned diagnostics with an explicit lastname: %v", diags)
	}
	if firstname != "Rosario" || lastname != "de Silva" {
		t.Errorf("resolveName = %q / %q, want Rosario / de Silva", firstname, lastname)
	}
}

func TestResolveNameFromExplicitOnly(t *testing.T) {
	t.Parallel()

	model := minimalModel()
	model.Name = types.StringNull()
	model.Firstname = types.StringValue("Mogens")
	model.Lastname = types.StringValue("Jensen")

	firstname, lastname, diags := resolveName(model)
	if diags.HasError() {
		t.Fatalf("resolveName returned diagnostics: %v", diags)
	}
	if firstname != "Mogens" || lastname != "Jensen" {
		t.Errorf("resolveName = %q / %q, want Mogens / Jensen", firstname, lastname)
	}
}

func TestNormalizePhone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		raw         string
		countryCode string
		want        string
		wantErr     bool
	}{
		{name: "bare national number gains the default code", raw: "20202020", countryCode: "+45", want: "+4520202020"},
		{name: "already international is left alone", raw: "+4512121212", countryCode: "+45", want: "+4512121212"},
		{name: "foreign number keeps its own code", raw: "+4915112345678", countryCode: "+45", want: "+4915112345678"},
		{name: "spaces are stripped", raw: "20 20 20 20", countryCode: "+45", want: "+4520202020"},
		{name: "trunk prefix is preserved rather than guessed at", raw: "020202020", countryCode: "+45", want: "+45020202020"},
		{name: "bare number without a default code fails", raw: "20202020", countryCode: "", wantErr: true},
		{name: "too short for the API pattern", raw: "12", countryCode: "+4", wantErr: true},
		{name: "letters rejected", raw: "not-a-phone", countryCode: "+45", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, diags := normalizePhone(tt.raw, tt.countryCode, "emp-1")
			if tt.wantErr {
				if !diags.HasError() {
					t.Fatalf("normalizePhone(%q, %q) = %v, want an error", tt.raw, tt.countryCode, got)
				}
				return
			}
			if diags.HasError() {
				t.Fatalf("normalizePhone returned diagnostics: %v", diags)
			}
			if got == nil || *got != tt.want {
				t.Errorf("normalizePhone(%q, %q) = %v, want %q", tt.raw, tt.countryCode, got, tt.want)
			}
		})
	}
}

func TestNormalizePhoneEmptyIsNil(t *testing.T) {
	t.Parallel()

	got, diags := normalizePhone("", "+45", "emp-1")
	if diags.HasError() {
		t.Fatalf("normalizePhone returned diagnostics: %v", diags)
	}
	if got != nil {
		t.Errorf("normalizePhone(\"\") = %v, want nil", got)
	}
}

func TestToAPIEmployeeMapsRequiredFields(t *testing.T) {
	t.Parallel()

	got, diags := toAPIEmployee(context.Background(), minimalModel(), "")
	if diags.HasError() {
		t.Fatalf("toAPIEmployee returned diagnostics: %v", diags)
	}

	if got.EmployeeID != "mj@example.dk" {
		t.Errorf("EmployeeID = %q", got.EmployeeID)
	}
	if got.Firstname != "Mogens" || got.Lastname != "Jensen" {
		t.Errorf("name = %q %q, want Mogens Jensen", got.Firstname, got.Lastname)
	}
	if got.EmploymentStatus != 0 {
		t.Errorf("EmploymentStatus = %d, want 0 for active", got.EmploymentStatus)
	}
	if got.Gender != nil {
		t.Errorf("Gender = %v, want nil when unset", got.Gender)
	}
}

func TestToAPIEmployeeInactiveIsOnLeave(t *testing.T) {
	t.Parallel()

	model := minimalModel()
	model.Active = types.BoolValue(false)

	got, diags := toAPIEmployee(context.Background(), model, "")
	if diags.HasError() {
		t.Fatalf("toAPIEmployee returned diagnostics: %v", diags)
	}
	if got.EmploymentStatus != 1 {
		t.Errorf("EmploymentStatus = %d, want 1 for on leave", got.EmploymentStatus)
	}
}

func TestToAPIEmployeeNormalizesPhone(t *testing.T) {
	t.Parallel()

	model := minimalModel()
	model.Phone = types.StringValue("20202020")

	got, diags := toAPIEmployee(context.Background(), model, "+45")
	if diags.HasError() {
		t.Fatalf("toAPIEmployee returned diagnostics: %v", diags)
	}
	if got.Phonenumber == nil || *got.Phonenumber != "+4520202020" {
		t.Errorf("Phonenumber = %v, want +4520202020", got.Phonenumber)
	}
	if got.ContactNumber != nil {
		t.Error("ContactNumber set on a write payload; it is a read-only field")
	}
}

func TestToAPIEmployeeHoistsWellKnownDimensions(t *testing.T) {
	t.Parallel()

	model := minimalModel()
	model.Dimensions = stringMap(t, map[string]string{
		"Department":       "Adventuring",
		"Role":             "consultant",
		"ImmediateManager": "Gandalf",
		"Location":         "copenhagen",
	})

	got, diags := toAPIEmployee(context.Background(), model, "")
	if diags.HasError() {
		t.Fatalf("toAPIEmployee returned diagnostics: %v", diags)
	}

	if got.Department == nil || *got.Department != "Adventuring" {
		t.Errorf("Department = %v, want it hoisted to top level", got.Department)
	}
	if got.Role == nil || *got.Role != "consultant" {
		t.Errorf("Role = %v, want it hoisted to top level", got.Role)
	}
	if got.ImmediateManager == nil || *got.ImmediateManager != "Gandalf" {
		t.Errorf("ImmediateManager = %v, want it hoisted to top level", got.ImmediateManager)
	}
	if got.Dimensions["Location"] != "copenhagen" {
		t.Errorf("Dimensions[Location] = %q, want copenhagen", got.Dimensions["Location"])
	}
	for _, key := range hoistedDimensionKeys {
		if _, ok := got.Dimensions[key]; ok {
			t.Errorf("Dimensions unexpectedly still contains hoisted key %q", key)
		}
	}
}

func TestFromAPIEmployeeWithoutPriorReportsAPIView(t *testing.T) {
	t.Parallel()

	department := "Adventuring"
	contact := "+4512121212"
	gender := 1

	api := wellbeingclient.Employee{
		EmployeeID:                "mj@example.dk",
		Firstname:                 "Mogens",
		Lastname:                  "Jensen",
		Email:                     "mj@example.dk",
		EmploymentStatus:          1,
		Gender:                    &gender,
		ContactNumber:             &contact,
		Department:                &department,
		Dimensions:                map[string]string{"Location": "copenhagen"},
		ID:                        254799,
		CompanyID:                 1000,
		Fullname:                  "Mogens Jensen",
		ExternalID:                "ext-1",
		Locked:                    true,
		SourcedFromExternalSystem: true,
	}

	got, diags := fromAPIEmployee(context.Background(), api, nil, "")
	if diags.HasError() {
		t.Fatalf("fromAPIEmployee returned diagnostics: %v", diags)
	}

	// With no config to echo, the API's own values are reported.
	if got.Firstname.ValueString() != "Mogens" || got.Lastname.ValueString() != "Jensen" {
		t.Errorf("name = %q / %q, want the API values", got.Firstname.ValueString(), got.Lastname.ValueString())
	}
	if !got.Name.IsNull() {
		t.Errorf("Name = %v, want null — the API has no single-name field", got.Name)
	}
	if got.Phone.ValueString() != "+4512121212" {
		t.Errorf("Phone = %q, want the value from ContactNumber", got.Phone.ValueString())
	}
	if got.Active.ValueBool() {
		t.Error("Active = true, want false for employment status 1")
	}
	if got.Gender.ValueString() != "female" {
		t.Errorf("Gender = %q, want female", got.Gender.ValueString())
	}
	if got.WellbeingID.ValueInt64() != 254799 {
		t.Errorf("WellbeingID = %d, want 254799", got.WellbeingID.ValueInt64())
	}

	dimensions := got.Dimensions.Elements()
	if len(dimensions) != 2 {
		t.Fatalf("Dimensions = %v, want Location plus the hoisted Department", dimensions)
	}
	if dimensions["Department"].(types.String).ValueString() != "Adventuring" {
		t.Errorf("Dimensions[Department] = %v, want it folded back in", dimensions["Department"])
	}
}

func TestFromAPIEmployeeCarriesConfiguredFormsForward(t *testing.T) {
	t.Parallel()

	// name, phone and invitation_date cannot round trip: the API splits the
	// name, normalises the phone, and never returns the invitation date. Echoing
	// the API's version would diff against config on every plan.
	prior := minimalModel()
	prior.Phone = types.StringValue("12121212")
	prior.InvitationDate = types.StringValue("2026-09-01T08:00:00Z")

	normalized := "+4512121212"
	api := wellbeingclient.Employee{
		EmployeeID:       "mj@example.dk",
		Firstname:        "Mogens",
		Lastname:         "Jensen",
		Email:            "mj@example.dk",
		EmploymentStatus: 0,
		ContactNumber:    &normalized,
	}

	got, diags := fromAPIEmployee(context.Background(), api, &prior, "+45")
	if diags.HasError() {
		t.Fatalf("fromAPIEmployee returned diagnostics: %v", diags)
	}

	if got.Name.ValueString() != "Mogens Jensen" {
		t.Errorf("Name = %q, want the configured single name carried forward", got.Name.ValueString())
	}
	if !got.Firstname.IsNull() || !got.Lastname.IsNull() {
		t.Errorf("firstname/lastname = %v / %v, want null — config never set them",
			got.Firstname, got.Lastname)
	}
	if got.Phone.ValueString() != "12121212" {
		t.Errorf("Phone = %q, want the configured bare number, not the normalised form", got.Phone.ValueString())
	}
	if got.InvitationDate.ValueString() != "2026-09-01T08:00:00Z" {
		t.Errorf("InvitationDate = %q, want it carried forward", got.InvitationDate.ValueString())
	}
}

func TestFromAPIEmployeeNullsEmptyDimensions(t *testing.T) {
	t.Parallel()

	api := wellbeingclient.Employee{EmployeeID: "emp-1", EmploymentStatus: 0}

	got, diags := fromAPIEmployee(context.Background(), api, nil, "")
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
		"Location":         "copenhagen",
	})

	api, diags := toAPIEmployee(context.Background(), model, "")
	if diags.HasError() {
		t.Fatalf("toAPIEmployee returned diagnostics: %v", diags)
	}

	// Simulate the server: GET returns every dimension nested, including the
	// ones the PUT accepted as top-level fields.
	served := wellbeingclient.Employee{
		EmployeeID:       api.EmployeeID,
		Firstname:        api.Firstname,
		Lastname:         api.Lastname,
		Email:            api.Email,
		EmploymentStatus: api.EmploymentStatus,
		Dimensions: map[string]string{
			"Department":       "Adventuring",
			"ImmediateManager": "Gandalf",
			"Location":         "copenhagen",
		},
	}

	got, diags := fromAPIEmployee(context.Background(), served, &model, "+45")
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

func TestFromAPIEmployeePreservesExplicitlyEmptyDimensions(t *testing.T) {
	t.Parallel()

	// Deriving dimensions from a roles list yields {} for anyone matching no
	// candidate. Config holds a known empty map while the API returns nothing,
	// so collapsing to null here would diff against config forever.
	prior := minimalModel()
	prior.Dimensions = stringMap(t, map[string]string{})

	api := wellbeingclient.Employee{
		EmployeeID:       "mj@example.dk",
		Firstname:        "Mogens",
		Lastname:         "Jensen",
		Email:            "mj@example.dk",
		EmploymentStatus: 0,
	}

	got, diags := fromAPIEmployee(context.Background(), api, &prior, "+45")
	if diags.HasError() {
		t.Fatalf("fromAPIEmployee returned diagnostics: %v", diags)
	}

	if got.Dimensions.IsNull() {
		t.Error("Dimensions = null, want an empty map to match the configured value")
	}
	if len(got.Dimensions.Elements()) != 0 {
		t.Errorf("Dimensions = %v, want empty", got.Dimensions)
	}
}
