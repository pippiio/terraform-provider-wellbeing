package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

const defaultOperationTimeout = 60 * time.Minute

var phoneNumberPattern = regexp.MustCompile(`^\+[0-9]{6,20}$`)

var (
	_ resource.Resource                = &employeeRosterResource{}
	_ resource.ResourceWithConfigure   = &employeeRosterResource{}
	_ resource.ResourceWithImportState = &employeeRosterResource{}
)

// NewEmployeeRosterResource returns the roster resource.
func NewEmployeeRosterResource() resource.Resource {
	return &employeeRosterResource{}
}

type employeeRosterResource struct {
	client *wellbeingclient.Client
}

type employeeRosterModel struct {
	ID                types.String   `tfsdk:"id"`
	BatchLimitPercent types.Int64    `tfsdk:"batch_limit_percent"`
	Employee          types.Map      `tfsdk:"employee"`
	Timeouts          timeouts.Value `tfsdk:"timeouts"`
}

func (r *employeeRosterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_employee_roster"
}

func (r *employeeRosterResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the company's entire employee roster.\n\n" +
			"The Wellbeing API has no per-employee endpoint: `PUT /Employee` replaces the whole set, " +
			"and employees absent from the payload are deleted. This resource therefore owns every " +
			"employee created through the API, and there is deliberately no `wellbeing_employee` resource.\n\n" +
			"~> **Destroying this resource does not delete anyone.** Terraform forgets the roster and " +
			"leaves Wellbeing untouched. Use `terraform import` to adopt an existing roster back into state.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The company ID this roster belongs to.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"batch_limit_percent": schema.Int64Attribute{
				MarkdownDescription: "Optional plan-time guard mirroring the company's configured batch limit. " +
					"When set, an apply that would change more than this percentage of the roster fails before " +
					"any request is sent. The API applies the same rule server-side but does not expose the " +
					"configured value, and a rejected import may only surface after a long queued wait. " +
					"The check is skipped when the company has fewer than 100 employees, matching the API.",
				Optional: true,
			},
			"employee": schema.MapNestedAttribute{
				MarkdownDescription: "The complete set of employees, keyed by your internal `EmployeeID`.",
				Required:            true,
				Validators: []validator.Map{
					mapvalidator.KeysAre(stringvalidator.LengthAtMost(50)),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"firstname": schema.StringAttribute{
							MarkdownDescription: "First name of the employee.",
							Required:            true,
							Validators:          []validator.String{stringvalidator.LengthAtMost(150)},
						},
						"lastname": schema.StringAttribute{
							MarkdownDescription: "Last name of the employee.",
							Required:            true,
							Validators:          []validator.String{stringvalidator.LengthAtMost(150)},
						},
						"email": schema.StringAttribute{
							MarkdownDescription: "E-mail address. Must be unique across the company.",
							Required:            true,
						},
						"employment_status": schema.StringAttribute{
							MarkdownDescription: "Employment status. One of `active` or `on_leave`.",
							Required:            true,
							Validators:          []validator.String{stringvalidator.OneOf(employmentStatusValues()...)},
						},
						"gender": schema.StringAttribute{
							MarkdownDescription: "Gender. One of `female`, `male` or `unknown`.",
							Optional:            true,
							Validators:          []validator.String{stringvalidator.OneOf(genderValues()...)},
						},
						"phone_number": schema.StringAttribute{
							MarkdownDescription: "Cell phone in international format, for example `+4523232323`. Must be unique.",
							Optional:            true,
							Validators: []validator.String{
								stringvalidator.RegexMatches(phoneNumberPattern, "must be a plus sign followed by 6 to 20 digits, for example +4523232323"),
							},
						},
						"job_title": schema.StringAttribute{
							MarkdownDescription: "Role in the company, for example `Sales Manager`.",
							Optional:            true,
							Validators:          []validator.String{stringvalidator.LengthAtMost(50)},
						},
						"invitation_date": schema.StringAttribute{
							MarkdownDescription: "When to send the invitation, formatted `yyyy-MM-ddTHH:mm:ssZ`. " +
								"Applies only to employees being created; the API never returns it, so the " +
								"configured value is carried forward in state unchanged.",
							Optional: true,
						},
						"dimensions": schema.MapAttribute{
							MarkdownDescription: "Reporting dimensions such as `Department`, `Role`, " +
								"`ImmediateManager`, `Location` or `Division`. The API accepts the first three " +
								"as dedicated fields and returns all of them nested here; the provider handles " +
								"that difference, so configure everything through this map.",
							Optional:    true,
							ElementType: types.StringType,
							Validators: []validator.Map{
								mapvalidator.ValueStringsAre(stringvalidator.LengthAtMost(50)),
							},
						},

						"id": schema.Int64Attribute{
							MarkdownDescription: "Wellbeing's internal numeric employee ID.",
							Computed:            true,
						},
						"fullname": schema.StringAttribute{
							MarkdownDescription: "Full name as assembled by Wellbeing.",
							Computed:            true,
						},
						"company_id": schema.Int64Attribute{
							MarkdownDescription: "Company the employee belongs to.",
							Computed:            true,
						},
						"external_id": schema.StringAttribute{
							MarkdownDescription: "External ID recorded by Wellbeing.",
							Computed:            true,
						},
						"first_invitation_date": schema.StringAttribute{
							MarkdownDescription: "When the first invitation was sent.",
							Computed:            true,
						},
						"locked": schema.BoolAttribute{
							MarkdownDescription: "Whether the employee's account is locked.",
							Computed:            true,
						},
						"opt_out": schema.BoolAttribute{
							MarkdownDescription: "Whether the employee has opted out of surveys.",
							Computed:            true,
						},
						"sourced_from_external_system": schema.BoolAttribute{
							MarkdownDescription: "Whether Wellbeing considers this employee externally sourced.",
							Computed:            true,
						},
					},
				},
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true}),
		},
	}
}

func (r *employeeRosterResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*wellbeingclient.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected resource configure type",
			fmt.Sprintf("Expected *wellbeingclient.Client, got %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *employeeRosterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan employeeRosterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, diags := plan.Timeouts.Create(ctx, defaultOperationTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	r.applyRoster(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *employeeRosterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state employeeRosterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	prior, diags := rosterToModels(ctx, state.Employee)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	employees, err := r.client.ListEmployees(ctx)
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to read Wellbeing employee roster", err)
		return
	}

	value, diags := rosterFromAPI(ctx, employees, prior)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.Employee = value
	state.ID = types.StringValue(r.client.CompanyID())

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *employeeRosterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan employeeRosterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, diags := plan.Timeouts.Update(ctx, defaultOperationTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	r.applyRoster(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes the roster from Terraform state without touching Wellbeing.
//
// The API has no delete endpoint; the only way to remove employees is to PUT a
// roster that omits them, which would wipe the entire workforce with no way to
// undo it. Destroying is therefore a state-only operation.
func (r *employeeRosterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state employeeRosterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddWarning(
		"Employees were not deleted from Wellbeing",
		fmt.Sprintf("Terraform has removed this roster from state, but all %d employees remain live in Wellbeing. "+
			"The API provides no delete operation, so the provider does not attempt one. "+
			"To manage this roster with Terraform again, run terraform import.",
			len(state.Employee.Elements()),
		),
	)
}

func (r *employeeRosterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The import ID is the company ID. Read fills in the roster itself.
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// applyRoster pushes the planned roster to the API, waits for a queued import to
// finish, and refreshes the plan's computed attributes from the result.
func (r *employeeRosterResource) applyRoster(ctx context.Context, plan *employeeRosterModel, diags *diag.Diagnostics) {
	planned, convertDiags := rosterToModels(ctx, plan.Employee)
	diags.Append(convertDiags...)
	if diags.HasError() {
		return
	}

	payload := make([]wellbeingclient.Employee, 0, len(planned))
	for _, employeeID := range slices.Sorted(maps.Keys(planned)) {
		employee, employeeDiags := toAPIEmployee(ctx, employeeID, planned[employeeID])
		diags.Append(employeeDiags...)
		if diags.HasError() {
			return
		}
		payload = append(payload, employee)
	}

	if !plan.BatchLimitPercent.IsNull() {
		current, err := r.client.ListEmployees(ctx)
		if err != nil {
			addAPIErrorDiagnostic(diags, "Unable to read the current Wellbeing roster", err)
			return
		}

		limit := int(plan.BatchLimitPercent.ValueInt64())
		if batchLimitExceeded(len(current), len(payload), limit) {
			diags.AddAttributeError(
				path.Root("batch_limit_percent"),
				"Change exceeds the configured batch limit",
				fmt.Sprintf("Applying would change the roster from %d to %d employees, which is at or above the "+
					"configured limit of %d%%. The Wellbeing API would reject or indefinitely queue this import. "+
					"Apply the change in smaller steps, or raise batch_limit_percent if the company's configured "+
					"limit is higher than the value set here.",
					len(current), len(payload), limit),
			)
			return
		}
	}

	result, err := r.client.ReplaceEmployees(ctx, payload)
	if err != nil {
		addAPIErrorDiagnostic(diags, "Unable to write the Wellbeing employee roster", err)
		return
	}

	tflog.Info(ctx, "submitted wellbeing employee roster", map[string]any{
		"api_operation_id": result.ApiOperationID,
		"inserted":         result.Inserted,
		"updated":          result.Updated,
		"removed":          result.Removed,
		"queued":           result.WasQueued.Bool(),
	})

	if result.WasQueued.Bool() {
		tflog.Info(ctx, "waiting for queued wellbeing import", map[string]any{
			"api_operation_id": result.ApiOperationID,
		})

		if err := r.client.WaitForOperation(ctx, result.ApiOperationID); err != nil {
			addAPIErrorDiagnostic(diags, "Queued Wellbeing import did not complete successfully", err)
			return
		}
	}

	// Refresh so computed attributes reflect what the API actually stored.
	employees, err := r.client.ListEmployees(ctx)
	if err != nil {
		addAPIErrorDiagnostic(diags, "Unable to read back the Wellbeing employee roster", err)
		return
	}

	value, refreshDiags := rosterFromAPI(ctx, employees, planned)
	diags.Append(refreshDiags...)
	if diags.HasError() {
		return
	}

	plan.Employee = value
	plan.ID = types.StringValue(r.client.CompanyID())
}

// rosterToModels decodes the employee map attribute into Go values.
func rosterToModels(ctx context.Context, value types.Map) (map[string]employeeModel, diag.Diagnostics) {
	models := map[string]employeeModel{}
	if value.IsNull() || value.IsUnknown() {
		return models, nil
	}

	diags := value.ElementsAs(ctx, &models, false)
	return models, diags
}

// rosterFromAPI rebuilds the employee map attribute from a GET response. prior
// supplies values the API never returns, such as invitation_date.
func rosterFromAPI(ctx context.Context, employees []wellbeingclient.Employee, prior map[string]employeeModel) (types.Map, diag.Diagnostics) {
	var diags diag.Diagnostics

	elementType := types.ObjectType{AttrTypes: employeeAttrTypes()}
	models := make(map[string]employeeModel, len(employees))

	for _, employee := range employees {
		var previous *employeeModel
		if existing, ok := prior[employee.EmployeeID]; ok {
			previous = &existing
		}

		model, employeeDiags := fromAPIEmployee(ctx, employee, previous)
		diags.Append(employeeDiags...)
		if diags.HasError() {
			return types.MapNull(elementType), diags
		}

		models[employee.EmployeeID] = model
	}

	value, mapDiags := types.MapValueFrom(ctx, elementType, models)
	diags.Append(mapDiags...)
	return value, diags
}

// addAPIErrorDiagnostic renders an APIError as one diagnostic per validation
// failure, keeping the operation id attached for support diagnostics.
func addAPIErrorDiagnostic(diags *diag.Diagnostics, summary string, err error) {
	var apiErr *wellbeingclient.APIError
	if !errors.As(err, &apiErr) {
		diags.AddError(summary, err.Error())
		return
	}

	if len(apiErr.ValidationErrors) == 0 {
		diags.AddError(summary, apiErr.Error())
		return
	}

	for _, validationError := range apiErr.ValidationErrors {
		detail := validationError
		if apiErr.ApiOperationID != "" {
			detail += fmt.Sprintf("\n\nApiOperationId: %s (quote this when contacting Wellbeing support)", apiErr.ApiOperationID)
		}
		diags.AddError(summary, detail)
	}
}
