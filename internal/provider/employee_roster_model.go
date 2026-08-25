package provider

import (
	"context"
	"math"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

// hoistedDimensionKeys are the dimension keys the API accepts as top-level PUT
// fields while returning them nested inside Dimensions on GET. The provider
// exposes only the dimensions map and reconciles the difference here.
var hoistedDimensionKeys = []string{"Department", "ImmediateManager", "Role"}

// employeeModel is one entry in the roster's employee map. The map key is the
// employee's EmployeeID, so it does not appear as an attribute.
type employeeModel struct {
	Firstname        types.String `tfsdk:"firstname"`
	Lastname         types.String `tfsdk:"lastname"`
	Email            types.String `tfsdk:"email"`
	EmploymentStatus types.String `tfsdk:"employment_status"`
	Gender           types.String `tfsdk:"gender"`
	PhoneNumber      types.String `tfsdk:"phone_number"`
	JobTitle         types.String `tfsdk:"job_title"`
	InvitationDate   types.String `tfsdk:"invitation_date"`
	Dimensions       types.Map    `tfsdk:"dimensions"`

	ID                        types.Int64  `tfsdk:"id"`
	Fullname                  types.String `tfsdk:"fullname"`
	CompanyID                 types.Int64  `tfsdk:"company_id"`
	ExternalID                types.String `tfsdk:"external_id"`
	FirstInvitationDate       types.String `tfsdk:"first_invitation_date"`
	Locked                    types.Bool   `tfsdk:"locked"`
	OptOut                    types.Bool   `tfsdk:"opt_out"`
	SourcedFromExternalSystem types.Bool   `tfsdk:"sourced_from_external_system"`
}

// employeeAttrTypes mirrors employeeModel for building the nested object type.
func employeeAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"firstname":         types.StringType,
		"lastname":          types.StringType,
		"email":             types.StringType,
		"employment_status": types.StringType,
		"gender":            types.StringType,
		"phone_number":      types.StringType,
		"job_title":         types.StringType,
		"invitation_date":   types.StringType,
		"dimensions":        types.MapType{ElemType: types.StringType},

		"id":                           types.Int64Type,
		"fullname":                     types.StringType,
		"company_id":                   types.Int64Type,
		"external_id":                  types.StringType,
		"first_invitation_date":        types.StringType,
		"locked":                       types.BoolType,
		"opt_out":                      types.BoolType,
		"sourced_from_external_system": types.BoolType,
	}
}

// toAPIEmployee converts one configured employee into a PUT payload entry.
func toAPIEmployee(ctx context.Context, employeeID string, model employeeModel) (wellbeingclient.Employee, diag.Diagnostics) {
	var diags diag.Diagnostics

	employee := wellbeingclient.Employee{
		EmployeeID: employeeID,
		Firstname:  model.Firstname.ValueString(),
		Lastname:   model.Lastname.ValueString(),
		Email:      model.Email.ValueString(),
	}

	status, ok := employmentStatusToAPI(model.EmploymentStatus.ValueString())
	if !ok {
		diags.AddError(
			"Invalid employment status",
			"Employee "+employeeID+" has employment_status "+model.EmploymentStatus.ValueString()+
				", which is not one of the values the Wellbeing API accepts.",
		)
		return employee, diags
	}
	employee.EmploymentStatus = status

	if !model.Gender.IsNull() && !model.Gender.IsUnknown() {
		gender, ok := genderToAPI(model.Gender.ValueString())
		if !ok {
			diags.AddError(
				"Invalid gender",
				"Employee "+employeeID+" has gender "+model.Gender.ValueString()+
					", which is not one of the values the Wellbeing API accepts.",
			)
			return employee, diags
		}
		employee.Gender = &gender
	}

	employee.Phonenumber = optionalString(model.PhoneNumber)
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
// prior is the matching entry from plan or state, or nil when none exists. It
// supplies values the API never returns.
func fromAPIEmployee(ctx context.Context, employee wellbeingclient.Employee, prior *employeeModel) (employeeModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	model := employeeModel{
		Firstname:                 types.StringValue(employee.Firstname),
		Lastname:                  types.StringValue(employee.Lastname),
		Email:                     types.StringValue(employee.Email),
		JobTitle:                  stringOrNull(employee.JobTitle),
		ID:                        types.Int64Value(employee.ID),
		Fullname:                  types.StringValue(employee.Fullname),
		CompanyID:                 types.Int64Value(employee.CompanyID),
		ExternalID:                types.StringValue(employee.ExternalID),
		FirstInvitationDate:       stringOrNull(employee.FirstInvitationDate),
		Locked:                    types.BoolValue(employee.Locked),
		OptOut:                    types.BoolValue(employee.OptOut),
		SourcedFromExternalSystem: types.BoolValue(employee.SourcedFromExternalSystem),

		// GET returns the phone number as ContactNumber, not Phonenumber.
		PhoneNumber: stringOrNull(employee.ContactNumber),

		// InvitationDate is write-only; carry the configured value forward so a
		// later plan does not show a phantom change.
		InvitationDate: types.StringNull(),
	}

	if prior != nil {
		model.InvitationDate = prior.InvitationDate
	}

	if name, ok := employmentStatusFromAPI(employee.EmploymentStatus); ok {
		model.EmploymentStatus = types.StringValue(name)
	} else {
		diags.AddError(
			"Unrecognised employment status from API",
			"Employee "+employee.EmployeeID+" has an employment status the provider does not know how to represent. "+
				"This usually means the Wellbeing API added a value; please report it to the provider developers.",
		)
		return model, diags
	}

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
