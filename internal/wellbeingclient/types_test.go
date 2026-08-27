package wellbeingclient

import (
	"encoding/json"
	"testing"
)

const employeeGETFixture = `{
  "Email": "test@example.com",
  "Birthday": null,
  "Gender": 1,
  "ExternalId": "12345678",
  "ContactNumber": "+4523232323",
  "HomeZipCode": "1000",
  "LanguageId": 1045,
  "JobTitle": "Burglar",
  "NotifyByEmail": true,
  "Dimensions": {
    "ImmediateManager": "John Doe",
    "Department": "2551",
    "Role": "1",
    "division": "Test"
  },
  "EmploymentStatus": 0,
  "Locked": false,
  "OptOut": false,
  "SourcedFromExternalSystem": true,
  "FirstInvitationDate": "2018-11-15T11:00:16.3627689",
  "Id": 254799,
  "Firstname": "David",
  "Lastname": "Rasmussen",
  "Fullname": "David Rasmussen",
  "CompanyId": 1000,
  "EmployeeID": "12345678"
}`

func TestEmployeeDecodesGETResponse(t *testing.T) {
	t.Parallel()

	var got Employee
	if err := json.Unmarshal([]byte(employeeGETFixture), &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if got.EmployeeID != "12345678" {
		t.Errorf("EmployeeID = %q, want %q", got.EmployeeID, "12345678")
	}
	if got.ID != 254799 {
		t.Errorf("ID = %d, want %d", got.ID, 254799)
	}
	if got.Fullname != "David Rasmussen" {
		t.Errorf("Fullname = %q, want %q", got.Fullname, "David Rasmussen")
	}
	if got.Gender == nil || *got.Gender != 1 {
		t.Errorf("Gender = %v, want 1", got.Gender)
	}
	if got.EmploymentStatus != 0 {
		t.Errorf("EmploymentStatus = %d, want 0", got.EmploymentStatus)
	}
	if got.ContactNumber == nil || *got.ContactNumber != "+4523232323" {
		t.Errorf("ContactNumber = %v, want +4523232323", got.ContactNumber)
	}
	if got.Dimensions["division"] != "Test" {
		t.Errorf("Dimensions[division] = %q, want %q", got.Dimensions["division"], "Test")
	}
	if !got.SourcedFromExternalSystem {
		t.Error("SourcedFromExternalSystem = false, want true")
	}
}

func TestEmployeeOmitsEmptyWriteFields(t *testing.T) {
	t.Parallel()

	// A minimal employee must not emit null optional fields — the API validates
	// Phonenumber against a regex and would reject an explicit null.
	e := Employee{
		EmployeeID:       "abc",
		Firstname:        "Bilbo",
		Lastname:         "Baggins",
		Email:            "bilbo@shire.test",
		EmploymentStatus: 0,
	}

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("re-Unmarshal returned error: %v", err)
	}

	for _, absent := range []string{"Phonenumber", "InvitationDate", "Gender", "JobTitle", "Department", "Role", "ImmediateManager", "Id", "Fullname", "ContactNumber"} {
		if _, ok := decoded[absent]; ok {
			t.Errorf("marshalled payload unexpectedly contains %q", absent)
		}
	}
	for _, present := range []string{"EmployeeID", "Firstname", "Lastname", "Email", "EmploymentStatus"} {
		if _, ok := decoded[present]; !ok {
			t.Errorf("marshalled payload missing required field %q", present)
		}
	}
}

func TestImportResultDecodesBothWasQueuedEncodings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "200 string encoding", input: `{"ApiOperationId":"op-1","Inserted":3,"Updated":1,"Removed":0,"WasQueued":"false"}`, want: false},
		{name: "202 bool encoding", input: `{"ApiOperationId":"op-2","Inserted":0,"Updated":0,"Removed":0,"WasQueued":true}`, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got ImportResult
			if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
				t.Fatalf("Unmarshal returned error: %v", err)
			}
			if got.WasQueued.Bool() != tt.want {
				t.Errorf("WasQueued = %v, want %v", got.WasQueued.Bool(), tt.want)
			}
		})
	}
}

func TestAPICallDecodesEmptyStatusCode(t *testing.T) {
	t.Parallel()

	var got APICall
	input := `{"ApiOperationId":"op-1","UserId":7,"Username":"api","CreatedOn":"2026-08-25T10:00:00","Method":"PUT","HttpStatusCode":"","HttpStatusReason":""}`
	if err := json.Unmarshal([]byte(input), &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.HttpStatusCode.Set {
		t.Errorf("HttpStatusCode.Set = true, want false for a freshly created operation")
	}
}
