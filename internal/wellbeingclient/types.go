package wellbeingclient

// Employee is the wire representation of a Wellbeing employee.
//
// GET and PUT are not symmetric. Fields are grouped below by which direction
// they travel; see the provider's employee_roster_model.go for how the
// asymmetries are reconciled into a single Terraform schema.
type Employee struct {
	// Read and write.
	EmployeeID       string            `json:"EmployeeID"`
	Firstname        string            `json:"Firstname"`
	Lastname         string            `json:"Lastname"`
	Email            string            `json:"Email"`
	EmploymentStatus int               `json:"EmploymentStatus"`
	Gender           *int              `json:"Gender,omitempty"`
	JobTitle         *string           `json:"JobTitle,omitempty"`
	Dimensions       map[string]string `json:"Dimensions,omitempty"`

	// Write only. PUT accepts these as top-level fields, but GET returns
	// Department, Role and ImmediateManager nested inside Dimensions, and
	// returns the phone number as ContactNumber rather than Phonenumber.
	Phonenumber      *string `json:"Phonenumber,omitempty"`
	InvitationDate   *string `json:"InvitationDate,omitempty"`
	Department       *string `json:"Department,omitempty"`
	Role             *string `json:"Role,omitempty"`
	ImmediateManager *string `json:"ImmediateManager,omitempty"`

	// Read only. Server-assigned; never sent on PUT.
	ID                        int64   `json:"Id,omitempty"`
	CompanyID                 int64   `json:"CompanyId,omitempty"`
	Fullname                  string  `json:"Fullname,omitempty"`
	ExternalID                string  `json:"ExternalId,omitempty"`
	ContactNumber             *string `json:"ContactNumber,omitempty"`
	FirstInvitationDate       *string `json:"FirstInvitationDate,omitempty"`
	Locked                    bool    `json:"Locked,omitempty"`
	OptOut                    bool    `json:"OptOut,omitempty"`
	SourcedFromExternalSystem bool    `json:"SourcedFromExternalSystem,omitempty"`
}

// ImportResult is returned by PUT /Employee and PUT /ManagerImport.
type ImportResult struct {
	ApiOperationID string   `json:"ApiOperationId"`
	Inserted       int      `json:"Inserted"`
	Updated        int      `json:"Updated"`
	Removed        int      `json:"Removed"`
	WasQueued      FlexBool `json:"WasQueued"`
}

// APICall is one entry from GET /Company/{companyId}/ApiCalls.
//
// Lifecycle per the API documentation: HttpStatusCode is empty on creation,
// becomes 202 once validated and queued, stays 202 while processing, and
// finally becomes 200 on success or a 4xx/5xx on failure.
type APICall struct {
	ApiOperationID   string  `json:"ApiOperationId"`
	UserID           int64   `json:"UserId"`
	Username         string  `json:"Username"`
	CreatedOn        string  `json:"CreatedOn"`
	Method           string  `json:"Method"`
	HttpStatusCode   FlexInt `json:"HttpStatusCode"`
	HttpStatusReason string  `json:"HttpStatusReason"`
}

// Language is one entry from GET /Company/{companyId}/Language/Enabled.
type Language struct {
	ID    int64  `json:"id"`
	Label string `json:"label"`
	Code  string `json:"code"`
}

// SurveyTemplate is one entry from GET /Survey/Templates.
type SurveyTemplate struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// SurveyAnswer is one entry from GET /Survey/Answers. Selected-option answers
// are grouped with a Count and a null Time; free-text answers always have
// Count 1 and carry the answer time.
type SurveyAnswer struct {
	SurveyID    int64   `json:"SurveyId"`
	QuestionKey string  `json:"QuestionKey"`
	Answer      string  `json:"Answer"`
	Time        *string `json:"Time"`
	Period      string  `json:"Period"`
	Count       int64   `json:"Count"`
}
