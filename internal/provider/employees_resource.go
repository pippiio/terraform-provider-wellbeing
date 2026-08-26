package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

const (
	defaultOperationTimeout = 60 * time.Minute

	// defaultBatchLimitPercent matches the value the Wellbeing API documentation
	// recommends configuring as the company's batch limit in the portal.
	defaultBatchLimitPercent = 25
)

var (
	_ resource.Resource                   = &employeesResource{}
	_ resource.ResourceWithConfigure      = &employeesResource{}
	_ resource.ResourceWithImportState    = &employeesResource{}
	_ resource.ResourceWithValidateConfig = &employeesResource{}
)

// NewEmployeesResource returns the employees resource.
func NewEmployeesResource() resource.Resource {
	return &employeesResource{}
}

type employeesResource struct {
	client *wellbeingclient.Client
}

type employeesResourceModel struct {
	ID                 types.String    `tfsdk:"id"`
	BatchLimitPercent  types.Int64     `tfsdk:"batch_limit_percent"`
	DefaultCountryCode types.String    `tfsdk:"default_country_code"`
	Employee           []employeeModel `tfsdk:"employee"`
	Timeouts           timeouts.Value  `tfsdk:"timeouts"`
}

func (r *employeesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_employees"
}

func (r *employeesResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the company's entire employee roster.\n\n" +
			"The Wellbeing API has no per-employee endpoint: `PUT /Employee` replaces the whole set, " +
			"and employees absent from the payload are deleted. This resource therefore owns every " +
			"employee created through the API, and there is deliberately no single-employee resource.\n\n" +
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
			"default_country_code": schema.StringAttribute{
				MarkdownDescription: "Country code prefixed to any `phone` that does not already start with `+`, " +
					"for example `+45`. Lets employees carry national numbers while the API receives the " +
					"international form it requires.\n\n" +
					"Digits are not otherwise rewritten: a national trunk prefix is left alone, because " +
					"stripping it correctly depends on the country. Without this set, every `phone` must " +
					"already be in full international form.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(countryCodePattern, "must be a plus sign followed by 1 to 4 digits, for example +45"),
				},
			},
			"batch_limit_percent": schema.Int64Attribute{
				MarkdownDescription: "Guard mirroring the company's configured batch limit. An apply that " +
					"would change this percentage or more of the roster fails before any request is sent. " +
					"The API applies the same rule server-side but does not expose the configured value, " +
					"and a rejected import may only surface after a long queued wait.\n\n" +
					"Defaults to `25`, the value the Wellbeing API documentation recommends configuring in " +
					"the portal. Raise it if the company's configured limit is higher, or set it to `0` to " +
					"disable the check entirely.\n\n" +
					"The check is skipped when the company has fewer than 100 employees, matching the API, " +
					"so an initial import into an empty company is never blocked.",
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(defaultBatchLimitPercent),
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
			},
		},
		Blocks: map[string]schema.Block{
			"employee": schema.ListNestedBlock{
				MarkdownDescription: "One block per employee. The complete set of blocks is the roster; " +
					"anyone omitted is deleted by the API on the next apply.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Your internal primary key for this employee, unique within the roster. " +
								"Sent to the API as `EmployeeID`.",
							Required:   true,
							Validators: []validator.String{stringvalidator.LengthAtMost(50)},
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Full name, split at the first space into first and last name. " +
								"`\"Rosario de Silva\"` becomes `Rosario` / `de Silva`.\n\n" +
								"Set `firstname` or `lastname` to override the split for a name it gets wrong. " +
								"A single-word name leaves no surname, which the API rejects, so those need an " +
								"explicit `lastname`.",
							Optional: true,
						},
						"firstname": schema.StringAttribute{
							MarkdownDescription: "First name, overriding whatever `name` would split to.",
							Optional:            true,
							Validators:          []validator.String{stringvalidator.LengthAtMost(maxNameLength)},
						},
						"lastname": schema.StringAttribute{
							MarkdownDescription: "Last name, overriding whatever `name` would split to.",
							Optional:            true,
							Validators:          []validator.String{stringvalidator.LengthAtMost(maxNameLength)},
						},
						"email": schema.StringAttribute{
							MarkdownDescription: "E-mail address. Must be unique across the company.",
							Required:            true,
						},
						"phone": schema.StringAttribute{
							MarkdownDescription: "Cell phone. Either full international form (`+4512345678`) or a " +
								"national number that `default_country_code` completes. Must be unique.",
							Optional: true,
						},
						"active": schema.BoolAttribute{
							MarkdownDescription: "Whether the employee is actively employed. `false` marks them " +
								"as on leave. Defaults to `true`.",
							Optional: true,
							Computed: true,
							Default:  booldefault.StaticBool(true),
						},
						"gender": schema.StringAttribute{
							MarkdownDescription: "Gender. One of `female`, `male` or `unknown`.",
							Optional:            true,
							Validators:          []validator.String{stringvalidator.OneOf(genderValues()...)},
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
								"`ImmediateManager`, `Location` or `Division`. The keys are what Wellbeing " +
								"reports group by and what survey selection rules filter on.\n\n" +
								"The API accepts the first three as dedicated fields and returns all of them " +
								"nested here; the provider handles that difference, so configure everything " +
								"through this map.",
							Optional:    true,
							ElementType: types.StringType,
							Validators: []validator.Map{
								mapvalidator.ValueStringsAre(stringvalidator.LengthAtMost(50)),
							},
						},

						"wellbeing_id": schema.Int64Attribute{
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
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true}),
		},
	}
}

// ValidateConfig reports name, phone and duplicate-id problems at plan time
// rather than after an apply has already started talking to the API.
func (r *employeesResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config employeesResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	countryCode := config.DefaultCountryCode.ValueString()
	if config.DefaultCountryCode.IsUnknown() {
		// Cannot judge bare phone numbers without knowing the prefix.
		countryCode = ""
	}

	seen := make(map[string]int, len(config.Employee))

	for index, employee := range config.Employee {
		blockPath := path.Root("employee").AtListIndex(index)

		if !employee.ID.IsNull() && !employee.ID.IsUnknown() {
			identifier := employee.ID.ValueString()
			if previous, duplicated := seen[identifier]; duplicated {
				resp.Diagnostics.AddAttributeError(
					blockPath.AtName("id"),
					"Duplicate employee id",
					fmt.Sprintf("Employee id %q is already used by the block at index %d. "+
						"Each id must be unique: it is the key the Wellbeing API matches employees on, "+
						"so duplicates would silently collapse into a single employee.",
						identifier, previous),
				)
			} else {
				seen[identifier] = index
			}
		}

		// Name resolution needs both the split source and any overrides to be
		// known; skip the check when they are not resolved yet.
		if employee.Name.IsUnknown() || employee.Firstname.IsUnknown() || employee.Lastname.IsUnknown() {
			continue
		}
		if _, _, diags := resolveName(employee); diags.HasError() {
			for _, d := range diags.Errors() {
				resp.Diagnostics.AddAttributeError(blockPath.AtName("name"), d.Summary(), d.Detail())
			}
		}

		if raw := optionalString(employee.Phone); raw != nil && !config.DefaultCountryCode.IsUnknown() {
			if _, diags := normalizePhone(*raw, countryCode, employee.ID.ValueString()); diags.HasError() {
				for _, d := range diags.Errors() {
					resp.Diagnostics.AddAttributeError(blockPath.AtName("phone"), d.Summary(), d.Detail())
				}
			}
		}
	}
}

func (r *employeesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *employeesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan employeesResourceModel
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

	r.applyEmployees(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *employeesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state employeesResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	employees, err := r.client.ListEmployees(ctx)
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to read Wellbeing employees", err)
		return
	}

	refreshed, diags := employeesFromAPIOrdered(ctx, employees, state.Employee)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.Employee = refreshed
	state.ID = types.StringValue(r.client.CompanyID())

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *employeesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan employeesResourceModel
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

	r.applyEmployees(ctx, &plan, &resp.Diagnostics)
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
func (r *employeesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state employeesResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddWarning(
		"Employees were not deleted from Wellbeing",
		fmt.Sprintf("Terraform has removed this roster from state, but all %d employees remain live in Wellbeing. "+
			"The API provides no delete operation, so the provider does not attempt one. "+
			"To manage this roster with Terraform again, run terraform import.",
			len(state.Employee),
		),
	)
}

func (r *employeesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The import ID is the company ID. Read fills in the employees themselves.
	if r.client.CompanyID() != req.ID {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"The import ID must be the company ID.",
		)
		return
	}

	if req.ID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"The import ID must be the company ID.",
		)
		return
	}
	
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// applyEmployees pushes the planned roster to the API, waits for a queued import
// to finish, and refreshes the plan's computed attributes from the result.
func (r *employeesResource) applyEmployees(ctx context.Context, plan *employeesResourceModel, diags *diag.Diagnostics) {
	countryCode := plan.DefaultCountryCode.ValueString()

	// Sort by id so the payload is stable regardless of block order; the API
	// treats the roster as a set, so ordering carries no meaning to it.
	planned := slices.Clone(plan.Employee)
	slices.SortFunc(planned, func(a, b employeeModel) int {
		return strings.Compare(a.ID.ValueString(), b.ID.ValueString())
	})

	payload := make([]wellbeingclient.Employee, 0, len(planned))
	for _, employee := range planned {
		converted, employeeDiags := toAPIEmployee(ctx, employee, countryCode)
		diags.Append(employeeDiags...)
		if diags.HasError() {
			return
		}
		payload = append(payload, converted)
	}

	// A limit of zero disables the guard; the attribute defaults to 25, so null
	// only occurs for state written before the attribute gained a default.
	if limit := int(plan.BatchLimitPercent.ValueInt64()); !plan.BatchLimitPercent.IsNull() && limit > 0 {
		current, err := r.client.ListEmployees(ctx)
		if err != nil {
			addAPIErrorDiagnostic(diags, "Unable to read the current Wellbeing roster", err)
			return
		}

		if batchLimitExceeded(len(current), len(payload), limit) {
			diags.AddAttributeError(
				path.Root("batch_limit_percent"),
				"Change exceeds the configured batch limit",
				fmt.Sprintf("Applying would change the roster from %d to %d employees, which is at or above the "+
					"configured limit of %d%%. The Wellbeing API would reject or indefinitely queue this import. "+
					"Apply the change in smaller steps, raise batch_limit_percent if the company's configured "+
					"limit is higher than the value set here, or set it to 0 to disable this check.",
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

	// Refresh so computed attributes reflect what the API actually stored,
	// keeping the configured block order.
	employees, err := r.client.ListEmployees(ctx)
	if err != nil {
		addAPIErrorDiagnostic(diags, "Unable to read back the Wellbeing employee roster", err)
		return
	}

	refreshed, refreshDiags := employeesFromAPIOrdered(ctx, employees, plan.Employee)
	diags.Append(refreshDiags...)
	if diags.HasError() {
		return
	}

	plan.Employee = refreshed
	plan.ID = types.StringValue(r.client.CompanyID())
}

// employeesFromAPIOrdered rebuilds the employee blocks from a GET response while
// preserving the configured order.
//
// Order matters only to Terraform: a list block that came back in a different
// order than it was written would diff on every plan. Employees the API returns
// that config does not mention are appended in id order, so they surface as
// drift the next plan removes.
func employeesFromAPIOrdered(ctx context.Context, apiEmployees []wellbeingclient.Employee, prior []employeeModel) ([]employeeModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	remaining := make(map[string]wellbeingclient.Employee, len(apiEmployees))
	for _, employee := range apiEmployees {
		remaining[employee.EmployeeID] = employee
	}

	result := make([]employeeModel, 0, len(apiEmployees))

	for i := range prior {
		identifier := prior[i].ID.ValueString()

		employee, ok := remaining[identifier]
		if !ok {
			// Configured but absent from the API: dropped upstream, so let it
			// disappear from state and be recreated by the next apply.
			continue
		}
		delete(remaining, identifier)

		model, employeeDiags := fromAPIEmployee(ctx, employee, &prior[i])
		diags.Append(employeeDiags...)
		if diags.HasError() {
			return nil, diags
		}
		result = append(result, model)
	}

	for _, identifier := range slices.Sorted(maps.Keys(remaining)) {
		model, employeeDiags := fromAPIEmployee(ctx, remaining[identifier], nil)
		diags.Append(employeeDiags...)
		if diags.HasError() {
			return nil, diags
		}
		result = append(result, model)
	}

	return result, diags
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
