package wellbeingclient

import (
	"strings"
	"testing"
)

func TestParseAPIErrorValidationErrors(t *testing.T) {
	t.Parallel()

	body := `{"ApiOperationId":"op-42","ValidationErrors":["Email is not unique","Phonenumber is malformed"]}`
	got := parseAPIError(400, []byte(body))

	if got.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", got.StatusCode)
	}
	if got.ApiOperationID != "op-42" {
		t.Errorf("ApiOperationID = %q, want %q", got.ApiOperationID, "op-42")
	}
	if len(got.ValidationErrors) != 2 {
		t.Fatalf("ValidationErrors length = %d, want 2", len(got.ValidationErrors))
	}
	if got.ValidationErrors[0] != "Email is not unique" {
		t.Errorf("ValidationErrors[0] = %q", got.ValidationErrors[0])
	}
	if !strings.Contains(got.Error(), "op-42") {
		t.Errorf("Error() = %q, want it to mention the operation id", got.Error())
	}
}

func TestParseAPIErrorMessage(t *testing.T) {
	t.Parallel()

	got := parseAPIError(401, []byte(`{"Message":"Authorization has been denied for this request."}`))

	if got.Message != "Authorization has been denied for this request." {
		t.Errorf("Message = %q", got.Message)
	}
	if !strings.Contains(got.Error(), "Authorization has been denied") {
		t.Errorf("Error() = %q, want it to include the message", got.Error())
	}
}

func TestParseAPIErrorBareString(t *testing.T) {
	t.Parallel()

	// PUT /ManagerImport returns a bare JSON string on 400.
	got := parseAPIError(400, []byte(`"Company integration is not allowed"`))

	if got.Message != "Company integration is not allowed" {
		t.Errorf("Message = %q, want the bare string decoded", got.Message)
	}
}

func TestParseAPIErrorNonJSON(t *testing.T) {
	t.Parallel()

	got := parseAPIError(500, []byte("<html>gateway timeout</html>"))

	if got.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", got.StatusCode)
	}
	if !strings.Contains(got.Error(), "gateway timeout") {
		t.Errorf("Error() = %q, want it to fall back to the raw body", got.Error())
	}
}
