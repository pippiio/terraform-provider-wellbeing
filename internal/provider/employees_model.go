package provider

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

// maxNameLength is the API's limit on Firstname and Lastname.
const maxNameLength = 150

// phoneNumberPattern is the format the API requires: a plus sign followed by
// six to twenty digits.
var phoneNumberPattern = regexp.MustCompile(`^\+[0-9]{6,20}$`)

// countryCodePattern validates default_country_code.
var countryCodePattern = regexp.MustCompile(`^\+[0-9]{1,4}$`)

// normalizePhone puts a configured phone number into the international form the
// API demands, prefixing defaultCountryCode when the number does not already
// carry one.
//
// Digits are never otherwise rewritten. A national trunk prefix (the leading 0
// in some countries) is left alone, because stripping it correctly depends on
// the country and guessing wrong silently dials the wrong person.
func normalizePhone(raw, defaultCountryCode, identifier string) (*string, diag.Diagnostics) {
	var diags diag.Diagnostics

	number := strings.ReplaceAll(strings.TrimSpace(raw), " ", "")
	if number == "" {
		return nil, diags
	}

	if !strings.HasPrefix(number, "+") {
		if defaultCountryCode == "" {
			diags.AddError(
				"Phone number has no country code",
				fmt.Sprintf("Employee %q has phone %q, which the Wellbeing API rejects because it lacks a country code.\n\n"+
					"Either write it in full international form (%q), or set default_country_code on the resource "+
					"so bare numbers are prefixed automatically.",
					identifier, raw, "+45"+number),
			)
			return nil, diags
		}
		number = defaultCountryCode + number
	}

	if !phoneNumberPattern.MatchString(number) {
		diags.AddError(
			"Invalid phone number",
			fmt.Sprintf("Employee %q resolves to phone %q, which is not a plus sign followed by 6 to 20 digits.",
				identifier, number),
		)
		return nil, diags
	}

	return &number, diags
}

// hoistedDimensionKeys are the dimension keys the API accepts as top-level PUT
// fields while returning them nested inside Dimensions on GET. The provider
// exposes only the dimensions map and reconciles the difference here.
var hoistedDimensionKeys = []string{"Department", "ImmediateManager", "Role"}

// employeeModel is one `employee` block.
type employeeModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Firstname      types.String `tfsdk:"firstname"`
	Lastname       types.String `tfsdk:"lastname"`
	Email          types.String `tfsdk:"email"`
	Phone          types.String `tfsdk:"phone"`
	Active         types.Bool   `tfsdk:"active"`
	Gender         types.String `tfsdk:"gender"`
	JobTitle       types.String `tfsdk:"job_title"`
	InvitationDate types.String `tfsdk:"invitation_date"`
	Dimensions     types.Map    `tfsdk:"dimensions"`

	WellbeingID               types.Int64  `tfsdk:"wellbeing_id"`
	Fullname                  types.String `tfsdk:"fullname"`
	CompanyID                 types.Int64  `tfsdk:"company_id"`
	ExternalID                types.String `tfsdk:"external_id"`
	FirstInvitationDate       types.String `tfsdk:"first_invitation_date"`
	Locked                    types.Bool   `tfsdk:"locked"`
	OptOut                    types.Bool   `tfsdk:"opt_out"`
	SourcedFromExternalSystem types.Bool   `tfsdk:"sourced_from_external_system"`
}

// employeeAttrTypes mirrors employeeModel for building the nested object type
// used by the employees data source.
func employeeAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":              types.StringType,
		"name":            types.StringType,
		"firstname":       types.StringType,
		"lastname":        types.StringType,
		"email":           types.StringType,
		"phone":           types.StringType,
		"active":          types.BoolType,
		"gender":          types.StringType,
		"job_title":       types.StringType,
		"invitation_date": types.StringType,
		"dimensions":      types.MapType{ElemType: types.StringType},

		"wellbeing_id":                 types.Int64Type,
		"fullname":                     types.StringType,
		"company_id":                   types.Int64Type,
		"external_id":                  types.StringType,
		"first_invitation_date":        types.StringType,
		"locked":                       types.BoolType,
		"opt_out":                      types.BoolType,
		"sourced_from_external_system": types.BoolType,
	}
}

// splitName divides a full name at the first space: the first word becomes the
// first name and everything after it becomes the surname.
//
// Splitting at the first space rather than the last keeps compound surnames
// intact — "Anna Van der Berg" yields "Anna" and "Van der Berg" — which is the
// common case in the Nordics and the Netherlands. Names that do not follow that
// shape are handled by setting firstname and lastname explicitly.
func splitName(name string) (string, string) {
	trimmed := strings.TrimSpace(name)

	first, rest, found := strings.Cut(trimmed, " ")
	if !found {
		return trimmed, ""
	}
	return first, strings.TrimSpace(rest)
}

// resolveName determines the first and last name the API should receive.
//
// An explicit firstname or lastname always wins over the split of name, so a
// name the splitter gets wrong can be corrected without abandoning the short
// form for every other employee.
func resolveName(model employeeModel) (string, string, diag.Diagnostics) {
	var diags diag.Diagnostics

	identifier := model.ID.ValueString()
	if identifier == "" {
		identifier = "(unknown id)"
	}

	var splitFirst, splitLast string
	if !model.Name.IsNull() && !model.Name.IsUnknown() {
		splitFirst, splitLast = splitName(model.Name.ValueString())
	}

	firstname := splitFirst
	if value := optionalString(model.Firstname); value != nil {
		firstname = *value
	}

	lastname := splitLast
	if value := optionalString(model.Lastname); value != nil {
		lastname = *value
	}

	if firstname == "" {
		diags.AddError(
			"Employee is missing a first name",
			fmt.Sprintf("Employee %q has no usable first name. Set name to a full name, or set firstname explicitly.", identifier),
		)
	}

	if lastname == "" {
		diags.AddError(
			"Employee is missing a last name",
			fmt.Sprintf("Employee %q has no usable last name. The Wellbeing API requires one.\n\n"+
				"name is split at the first space, so a single-word name such as %q leaves no surname. "+
				"Either give name a full name, or set lastname explicitly for this employee.",
				identifier, model.Name.ValueString()),
		)
	}

	for label, value := range map[string]string{"first name": firstname, "last name": lastname} {
		if len(value) > maxNameLength {
			diags.AddError(
				"Employee name is too long",
				fmt.Sprintf("Employee %q has a %s of %d characters; the Wellbeing API allows at most %d.",
					identifier, label, len(value), maxNameLength),
			)
		}
	}

	return firstname, lastname, diags
}

// toAPIEmployee converts one configured employee into a PUT payload entry.
func toAPIEmployee(ctx context.Context, model employeeModel, defaultCountryCode string) (wellbeingclient.Employee, diag.Diagnostics) {
	var diags diag.Diagnostics

	employee := wellbeingclient.Employee{
		EmployeeID: model.ID.ValueString(),
		Email:      model.Email.ValueString(),
	}

	firstname, lastname, nameDiags := resolveName(model)
	diags.Append(nameDiags...)
	if diags.HasError() {
		return employee, diags
	}
	employee.Firstname = firstname
	employee.Lastname = lastname

	// active is a friendlier spelling of the API's EmploymentStatus enum; the
	// enum table stays the single source of truth for the numeric values.
	statusName := "on_leave"
	if model.Active.IsNull() || model.Active.ValueBool() {
		statusName = "active"
	}
	status, ok := employmentStatusToAPI(statusName)
	if !ok {
		diags.AddError(
			"Unknown employment status",
			"The provider could not map active to an employment status. Please report this issue to the provider developers.",
		)
		return employee, diags
	}
	employee.EmploymentStatus = status

	if !model.Gender.IsNull() && !model.Gender.IsUnknown() {
		gender, ok := genderToAPI(model.Gender.ValueString())
		if !ok {
			diags.AddError(
				"Invalid gender",
				"Employee "+model.ID.ValueString()+" has gender "+model.Gender.ValueString()+
					", which is not one of the values the Wellbeing API accepts.",
			)
			return employee, diags
		}
		employee.Gender = &gender
	}

	if raw := optionalString(model.Phone); raw != nil {
		phone, phoneDiags := normalizePhone(*raw, defaultCountryCode, model.ID.ValueString())
		diags.Append(phoneDiags...)
		if diags.HasError() {
			return employee, diags
		}
		employee.Phonenumber = phone
	}

	employee.JobTitle = optionalString(model.JobTitle)
	employee.InvitationDate = optionalString(model.InvitationDate)

	dimensions, dimDiags := mapToGo(ctx, model.Dimensions)
	diags.Append(dimDiags...)
	if diags.HasError() {
		return employee, diags
	}

	// Split the configured dimensions: the three well-known keys travel as
	// top-level fields, everything else stays in Dimensions.
	remaining := make(map[string]string, len(dimensions))
	for key, value := range dimensions {
		remaining[key] = value
	}

	for _, key := range hoistedDimensionKeys {
		value, ok := dimensions[key]
		if !ok {
			continue
		}
		delete(remaining, key)

		hoisted := value
		switch key {
		case "Department":
			employee.Department = &hoisted
		case "ImmediateManager":
			employee.ImmediateManager = &hoisted
		case "Role":
			employee.Role = &hoisted
		}
	}

	if len(remaining) > 0 {
		employee.Dimensions = remaining
	}

	return employee, diags
}

// fromAPIEmployee converts a GET response entry back into the Terraform model.
//
// prior is the matching configured block, or nil when none exists — the case for
// the data source and for employees the API returns that config does not
// mention. When prior is present its name and invitation_date are carried
// forward, because the API cannot report them in the form they were configured.
func fromAPIEmployee(ctx context.Context, employee wellbeingclient.Employee, prior *employeeModel) (employeeModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	model := employeeModel{
		ID:                        types.StringValue(employee.EmployeeID),
		Email:                     types.StringValue(employee.Email),
		JobTitle:                  stringOrNull(employee.JobTitle),
		WellbeingID:               types.Int64Value(employee.ID),
		Fullname:                  types.StringValue(employee.Fullname),
		CompanyID:                 types.Int64Value(employee.CompanyID),
		ExternalID:                types.StringValue(employee.ExternalID),
		FirstInvitationDate:       stringOrNull(employee.FirstInvitationDate),
		Locked:                    types.BoolValue(employee.Locked),
		OptOut:                    types.BoolValue(employee.OptOut),
		SourcedFromExternalSystem: types.BoolValue(employee.SourcedFromExternalSystem),

		// InvitationDate is write-only; carried forward from config below.
		InvitationDate: types.StringNull(),
	}

	if prior != nil {
		// The API reports Firstname and Lastname, but config may have expressed
		// them as a single name. Echoing the API's split back would diff against
		// a config that never set them, so the configured spelling wins.
		model.Name = prior.Name
		model.Firstname = prior.Firstname
		model.Lastname = prior.Lastname
		model.InvitationDate = prior.InvitationDate

		// Likewise for the phone: the API returns the normalised international
		// form, which would diff against a config holding a bare number.
		model.Phone = prior.Phone
	} else {
		// No config to echo — report what the API holds. GET returns the phone
		// number as ContactNumber rather than Phonenumber.
		model.Name = types.StringNull()
		model.Firstname = types.StringValue(employee.Firstname)
		model.Lastname = types.StringValue(employee.Lastname)
		model.Phone = stringOrNull(employee.ContactNumber)
	}

	statusName, ok := employmentStatusFromAPI(employee.EmploymentStatus)
	if !ok {
		diags.AddError(
			"Unrecognised employment status from API",
			"Employee "+employee.EmployeeID+" has an employment status the provider does not know how to represent. "+
				"This usually means the Wellbeing API added a value; please report it to the provider developers.",
		)
		return model, diags
	}
	model.Active = types.BoolValue(statusName == "active")

	model.Gender = types.StringNull()
	if employee.Gender != nil {
		name, ok := genderFromAPI(*employee.Gender)
		if !ok {
			diags.AddError(
				"Unrecognised gender from API",
				"Employee "+employee.EmployeeID+" has a gender value the provider does not know how to represent. "+
					"This usually means the Wellbeing API added a value; please report it to the provider developers.",
			)
			return model, diags
		}
		model.Gender = types.StringValue(name)
	}

	// Fold any top-level department fields back into the dimensions map so the
	// round trip matches what the user configured.
	dimensions := make(map[string]string, len(employee.Dimensions)+len(hoistedDimensionKeys))
	for key, value := range employee.Dimensions {
		dimensions[key] = value
	}
	for key, value := range map[string]*string{
		"Department":       employee.Department,
		"ImmediateManager": employee.ImmediateManager,
		"Role":             employee.Role,
	} {
		if value != nil && *value != "" {
			dimensions[key] = *value
		}
	}

	if len(dimensions) == 0 {
		model.Dimensions = types.MapNull(types.StringType)
		return model, diags
	}

	value, mapDiags := types.MapValueFrom(ctx, types.StringType, dimensions)
	diags.Append(mapDiags...)
	model.Dimensions = value

	return model, diags
}

// batchLimitExceeded reports whether replacing a roster of current employees
// with desired employees breaches the company's configured batch limit.
//
// The API rule is |x-y|/x*100 < z, with the check skipped entirely when there
// are fewer than 100 employees in the system.
func batchLimitExceeded(current, desired, limitPercent int) bool {
	if current < 100 {
		return false
	}

	change := math.Abs(float64(current-desired)) / float64(current) * 100
	return change >= float64(limitPercent)
}

func optionalString(value types.String) *string {
	if value.IsNull() || value.IsUnknown() || value.ValueString() == "" {
		return nil
	}
	result := value.ValueString()
	return &result
}

func stringOrNull(value *string) types.String {
	if value == nil || *value == "" {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

func mapToGo(ctx context.Context, value types.Map) (map[string]string, diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return map[string]string{}, nil
	}

	result := make(map[string]string, len(value.Elements()))
	diags := value.ElementsAs(ctx, &result, false)
	return result, diags
}

// configureDataSourceClient extracts the shared client from provider data.
func configureDataSourceClient(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *wellbeingclient.Client {
	if req.ProviderData == nil {
		return nil
	}

	client, ok := req.ProviderData.(*wellbeingclient.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected data source configure type",
			fmt.Sprintf("Expected *wellbeingclient.Client, got %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return nil
	}

	return client
}
