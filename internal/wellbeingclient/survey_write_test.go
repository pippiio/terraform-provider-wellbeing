package wellbeingclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// newSurveyClient wires a client to a stub server. Every survey test uses this
// rather than talking to the real API.
func newSurveyClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := NewClient(server.URL, "test-token", "1296")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestSurveyDecodesCapturedDetailResponse(t *testing.T) {
	// testdata/survey-detail.json is a verbatim capture from the UAT API, so a
	// decode failure here means the wire types have drifted from reality.
	raw, err := os.ReadFile("testdata/survey-detail.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	var survey Survey
	if err := json.Unmarshal(raw, &survey); err != nil {
		t.Fatalf("decoding captured response: %v", err)
	}

	if survey.ID == 0 {
		t.Error("ID was not decoded")
	}
	if survey.SurveyDefinitionID == 0 {
		t.Error("SurveyDefinitionID was not decoded; Labels cannot be resolved without it")
	}
	if len(survey.Questions) != 4 {
		t.Fatalf("Questions = %d, want 4", len(survey.Questions))
	}

	// The detail response omits CompanyId entirely.
	if survey.CompanyID != 0 {
		t.Errorf("CompanyID = %d, want 0 — the detail response does not carry it", survey.CompanyID)
	}
}

func TestSurveyQuestionCarriesServerKeyAndOrder(t *testing.T) {
	survey := loadFixture(t)

	first := survey.Questions[0]
	if first.Key == "" {
		t.Error("Key was not decoded; without it an update would duplicate the question")
	}
	if first.Order == 0 {
		t.Error("Order was not decoded")
	}
	if first.StepType != StepTypeWelcome {
		t.Errorf("StepType = %d, want %d (welcome)", first.StepType, StepTypeWelcome)
	}
}

func TestSurveyQuestionConfigurationIsAJSONString(t *testing.T) {
	// Configuration is a JSON-encoded string, not a nested object. Decoding it
	// requires a second Unmarshal.
	survey := loadFixture(t)

	var option *SurveyQuestion
	for i := range survey.Questions {
		if survey.Questions[i].StepType == StepTypeOption {
			option = &survey.Questions[i]
		}
	}
	if option == nil {
		t.Fatal("fixture has no option question")
	}
	if option.Configuration == nil {
		t.Fatal("option question has no Configuration")
	}

	config, err := option.DecodeConfiguration()
	if err != nil {
		t.Fatalf("DecodeConfiguration: %v", err)
	}
	if len(config.AnswerOptions) != 2 {
		t.Fatalf("AnswerOptions = %d, want 2", len(config.AnswerOptions))
	}
	if config.AnswerOptions[0].LabelKey == "" {
		t.Error("LabelKey was not decoded; without it an update would duplicate the option")
	}
}

func TestSurveyQuestionDecodeConfigurationHandlesAbsent(t *testing.T) {
	// Welcome, prompt and thankyou steps carry no Configuration at all.
	survey := loadFixture(t)

	config, err := survey.Questions[0].DecodeConfiguration()
	if err != nil {
		t.Fatalf("DecodeConfiguration on a nil Configuration: %v", err)
	}
	if len(config.AnswerOptions) != 0 {
		t.Errorf("AnswerOptions = %d, want 0", len(config.AnswerOptions))
	}
}

func TestSurveyLabelsAreKeyedByLanguageCode(t *testing.T) {
	// Writes address languages by numeric id; reads come back keyed by code.
	survey := loadFixture(t)

	if _, ok := survey.Labels["da"]; !ok {
		t.Fatalf("Labels has no 'da' entry; keys = %v", keysOf(survey.Labels))
	}
	if _, ok := survey.Labels["1041"]; ok {
		t.Error("Labels is keyed by numeric id, but reads should be keyed by code")
	}
}

func TestSurveyLabelPathMatchesTheCapturedFormat(t *testing.T) {
	survey := loadFixture(t)

	path := SurveyLabelPath(survey.SurveyDefinitionID, "Q2", SurveySlotQuestion)

	if _, ok := survey.Labels["da"][path]; !ok {
		t.Errorf("built path %q is absent from the captured Labels map", path)
	}
}

func TestSurveyTextLooksUpBySlot(t *testing.T) {
	survey := loadFixture(t)

	tests := []struct {
		name      string
		key, slot string
		want      string
	}{
		{name: "welcome uses Content", key: "Q1", slot: SurveySlotContent, want: "Velkommen probe"},
		{name: "question uses Question", key: "Q2", slot: SurveySlotQuestion, want: "Foerste spoergsmaal"},
		{name: "thankyou uses Content", key: "Q4", slot: SurveySlotContent, want: "Tak probe"},
		{name: "option uses OptionN", key: "Q2", slot: SurveySlotOption(1), want: "Ja"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := survey.Text("da", test.key, test.slot); got != test.want {
				t.Errorf("Text(da, %s, %s) = %q, want %q", test.key, test.slot, got, test.want)
			}
		})
	}
}

func TestSurveyTextReturnsEmptyForUnknownLanguage(t *testing.T) {
	survey := loadFixture(t)

	if got := survey.Text("fr", "Q2", SurveySlotQuestion); got != "" {
		t.Errorf("Text for an unenabled language = %q, want empty", got)
	}
}

func TestListSurveysFiltersByCompanyAndState(t *testing.T) {
	var gotQuery string
	client := newSurveyClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[{"Id":1,"Name":"One","State":2}]`))
	}))

	surveys, err := client.ListSurveys(context.Background(), StateActive)
	if err != nil {
		t.Fatalf("ListSurveys: %v", err)
	}

	if len(surveys) != 1 {
		t.Fatalf("got %d surveys, want 1", len(surveys))
	}
	for _, want := range []string{"companyId=1296", "State=2"} {
		if !contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestListSurveysOmitsStateWhenUnfiltered(t *testing.T) {
	var gotQuery string
	client := newSurveyClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[]`))
	}))

	if _, err := client.ListSurveys(context.Background(), 0); err != nil {
		t.Fatalf("ListSurveys: %v", err)
	}

	if contains(gotQuery, "State=") {
		t.Errorf("query %q should not filter by state when none is given", gotQuery)
	}
}

func TestGetSurveyRequestsTheDetailPath(t *testing.T) {
	var gotPath string
	raw, _ := os.ReadFile("testdata/survey-detail.json")
	client := newSurveyClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write(raw)
	}))

	survey, err := client.GetSurvey(context.Background(), 1710)
	if err != nil {
		t.Fatalf("GetSurvey: %v", err)
	}

	if gotPath != "/v1.0/Survey/1710" {
		t.Errorf("path = %q, want /v1.0/Survey/1710", gotPath)
	}
	if survey.ID != 1710 {
		t.Errorf("ID = %d, want 1710", survey.ID)
	}
}

func TestCreateSurveyReturnsTheBareIntegerID(t *testing.T) {
	// POST /Survey answers with a bare integer body, not a JSON object.
	var gotMethod, gotPath string
	client := newSurveyClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`1710`))
	}))

	id, err := client.CreateSurvey(context.Background(), Survey{Name: "probe"})
	if err != nil {
		t.Fatalf("CreateSurvey: %v", err)
	}

	if id != 1710 {
		t.Errorf("id = %d, want 1710", id)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1.0/Survey" {
		t.Errorf("got %s %s, want POST /v1.0/Survey", gotMethod, gotPath)
	}
}

func TestCreateSurveySendsCompanyID(t *testing.T) {
	var sent Survey
	client := newSurveyClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		_, _ = w.Write([]byte(`1`))
	}))

	if _, err := client.CreateSurvey(context.Background(), Survey{Name: "probe"}); err != nil {
		t.Fatalf("CreateSurvey: %v", err)
	}

	if sent.CompanyID != 1296 {
		t.Errorf("CompanyID = %d, want 1296 — the client must stamp it", sent.CompanyID)
	}
}

func TestUpdateSurveyPostsToTheUpdatePath(t *testing.T) {
	var gotPath string
	var sent Survey
	client := newSurveyClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&sent)
		w.WriteHeader(http.StatusOK)
	}))

	err := client.UpdateSurvey(context.Background(), Survey{
		ID:        1710,
		Questions: []SurveyQuestion{{Key: "Q1", StepType: StepTypeWelcome}},
	})
	if err != nil {
		t.Fatalf("UpdateSurvey: %v", err)
	}

	if gotPath != "/v1.0/Survey/Update" {
		t.Errorf("path = %q, want /v1.0/Survey/Update", gotPath)
	}
	if sent.ID != 1710 {
		t.Errorf("ID = %d, want 1710", sent.ID)
	}
}

func TestUpdateSurveyRejectsAnEmptyQuestionSet(t *testing.T) {
	// The API rejects this with an opaque 500. Failing client-side turns that
	// into an actionable message and saves a round trip.
	client := newSurveyClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should have been sent")
	}))

	err := client.UpdateSurvey(context.Background(), Survey{ID: 1710})

	if err == nil {
		t.Fatal("expected an error for an update carrying no questions")
	}
	if !contains(err.Error(), "question") {
		t.Errorf("error %q should mention the missing questions", err)
	}
}

func TestChangeSurveyStatePostsIDArray(t *testing.T) {
	var gotPath string
	var sent []int64
	client := newSurveyClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&sent)
		w.WriteHeader(http.StatusOK)
	}))

	if err := client.ChangeSurveyState(context.Background(), []int64{1710, 1711}, StateInactive); err != nil {
		t.Fatalf("ChangeSurveyState: %v", err)
	}

	if gotPath != "/v1.0/Survey/ChangeState/3" {
		t.Errorf("path = %q, want /v1.0/Survey/ChangeState/3", gotPath)
	}
	if len(sent) != 2 || sent[0] != 1710 {
		t.Errorf("body = %v, want [1710 1711]", sent)
	}
}

func TestSurveyErrorsSurfaceAsAPIError(t *testing.T) {
	// The survey endpoints answer with a bare text body and HTTP 500 rather than
	// the validation envelope the employee endpoints use.
	client := newSurveyClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`Survey must contain one cover page and one thank you page`))
	}))

	_, err := client.CreateSurvey(context.Background(), Survey{Name: "probe"})

	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatalf("error %v is not an *APIError", err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
	if !contains(apiErr.Error(), "cover page") {
		t.Errorf("error %q should carry the API's message", apiErr.Error())
	}
}

// --- small helpers, kept at the bottom so the tests above read cleanly ---

func loadFixture(t *testing.T) Survey {
	t.Helper()

	raw, err := os.ReadFile("testdata/survey-detail.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var survey Survey
	if err := json.Unmarshal(raw, &survey); err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	return survey
}

func keysOf(m map[string]map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func asAPIError(err error, target **APIError) bool {
	return errors.As(err, target)
}
