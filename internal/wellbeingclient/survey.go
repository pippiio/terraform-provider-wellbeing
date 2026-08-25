package wellbeingclient

import (
	"context"
	"net/http"
	"net/url"
)

// SurveyAnswerFilter narrows a survey answers query. All fields are optional;
// companyId is always supplied from the client.
type SurveyAnswerFilter struct {
	SurveyID     string
	DepartmentID string
	// Period is "YYYY-Q1".."YYYY-Q4" for quarterly surveys, otherwise "YYYY-M".
	Period string
}

// ListSurveyTemplates returns every survey template available to the company.
func (c *Client) ListSurveyTemplates(ctx context.Context) ([]SurveyTemplate, error) {
	var out []SurveyTemplate
	if _, err := c.do(ctx, http.MethodGet, "/v1.0/Survey/Templates", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListSurveyAnswers returns aggregated survey answers.
func (c *Client) ListSurveyAnswers(ctx context.Context, filter SurveyAnswerFilter) ([]SurveyAnswer, error) {
	query := url.Values{}
	query.Set("companyId", c.companyID)

	for key, value := range map[string]string{
		"surveyId":     filter.SurveyID,
		"departmentId": filter.DepartmentID,
		"period":       filter.Period,
	} {
		if value != "" {
			query.Set(key, value)
		}
	}

	var out []SurveyAnswer
	if _, err := c.do(ctx, http.MethodGet, "/v1.0/Survey/Answers?"+query.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
