package wellbeingclient

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

func TestListSurveyTemplates(t *testing.T) {
	t.Parallel()

	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/Survey/Templates" {
			t.Errorf("path = %q, want /v1.0/Survey/Templates", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":7,"name":"Onboarding"}]`))
	}))

	got, err := c.ListSurveyTemplates(context.Background())
	if err != nil {
		t.Fatalf("ListSurveyTemplates returned error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Onboarding" {
		t.Errorf("got = %+v, want one template named Onboarding", got)
	}
}

func TestListSurveyAnswersAlwaysSendsCompanyID(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/Survey/Answers" {
			t.Errorf("path = %q, want /v1.0/Survey/Answers", r.URL.Path)
		}
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`[]`))
	}))

	if _, err := c.ListSurveyAnswers(context.Background(), SurveyAnswerFilter{}); err != nil {
		t.Fatalf("ListSurveyAnswers returned error: %v", err)
	}
	if got := gotQuery.Get("companyId"); got != "1000" {
		t.Errorf("companyId = %q, want 1000", got)
	}
	for _, absent := range []string{"surveyId", "departmentId", "period"} {
		if _, ok := gotQuery[absent]; ok {
			t.Errorf("query unexpectedly contains empty filter %q", absent)
		}
	}
}

func TestListSurveyAnswersAppliesFilters(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`[{"SurveyId":1000,"QuestionKey":"Q2","Answer":"2","Time":null,"Period":"2020-4","Count":50}]`))
	}))

	got, err := c.ListSurveyAnswers(context.Background(), SurveyAnswerFilter{
		SurveyID:     "1000",
		DepartmentID: "55",
		Period:       "2020-Q4",
	})
	if err != nil {
		t.Fatalf("ListSurveyAnswers returned error: %v", err)
	}

	if gotQuery.Get("surveyId") != "1000" {
		t.Errorf("surveyId = %q, want 1000", gotQuery.Get("surveyId"))
	}
	if gotQuery.Get("departmentId") != "55" {
		t.Errorf("departmentId = %q, want 55", gotQuery.Get("departmentId"))
	}
	if gotQuery.Get("period") != "2020-Q4" {
		t.Errorf("period = %q, want 2020-Q4", gotQuery.Get("period"))
	}
	if len(got) != 1 || got[0].Count != 50 {
		t.Errorf("got = %+v, want one grouped answer with count 50", got)
	}
	if got[0].Time != nil {
		t.Errorf("Time = %v, want nil for a grouped option answer", got[0].Time)
	}
}
