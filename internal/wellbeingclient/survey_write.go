package wellbeingclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Survey step types. The API encodes each step of a survey as one of these.
const (
	StepTypeWelcome  = 1
	StepTypeOption   = 2
	StepTypePrompt   = 3
	StepTypeThankYou = 99
)

// Survey lifecycle states.
const (
	StateDraft    = 1
	StateActive   = 2
	StateInactive = 3
)

// Label slot names. Text does not live on the question itself: it sits in the
// survey's Labels map under a path built from the definition id, the question's
// server key, and one of these slots.
const (
	// SurveySlotContent holds the text of a welcome or thankyou page.
	SurveySlotContent = "Content"
	// SurveySlotQuestion holds the text of an option or prompt question.
	SurveySlotQuestion = "Question"
)

// SurveySlotOption names the slot holding the text of the nth answer option.
// The API numbers these from one.
func SurveySlotOption(n int) string {
	return "Option" + strconv.Itoa(n)
}

// SurveyLabelPath builds the key under which a step's text is stored.
//
// The definition id is the survey's SurveyDefinitionId, not its Id — the two
// differ, and using the wrong one silently yields no text.
func SurveyLabelPath(definitionID int64, questionKey, slot string) string {
	return fmt.Sprintf("SurveyDefinition.%d.Step.%s.%s", definitionID, questionKey, slot)
}

// Survey is the wire representation of a Wellbeing survey.
//
// Read and write shapes differ substantially, more so than for Employee:
//
//   - Text travels inline as Texts on each question when writing, but comes back
//     in the separate Labels map when reading.
//   - Writes address languages by numeric id ("1041"); reads key Labels by
//     language code ("da").
//   - The cover and thankyou pages are ordinary Questions entries with StepType
//     Welcome and ThankYou. The API also accepts top-level CoverPage and
//     ThankYouPage objects but ignores them, so this client does not model them.
//   - CompanyId appears on the list response but not on the detail response.
//
// Question identity is server-assigned and load-bearing: see SurveyQuestion.Key.
type Survey struct {
	// Read and write.
	Name                string `json:"Name"`
	StartDate           string `json:"StartDate,omitempty"`
	EndDate             string `json:"EndDate,omitempty"`
	State               int    `json:"State"`
	Frequency           int    `json:"Frequency"`
	SurveySelectionRule string `json:"SurveySelectionRule,omitempty"`

	// Read only. ID and SurveyDefinitionID are server-assigned; CompanyID is
	// returned by the list endpoint only.
	ID                 int64 `json:"Id,omitempty"`
	SurveyDefinitionID int64 `json:"SurveyDefinitionId,omitempty"`
	CompanyID          int64 `json:"CompanyId,omitempty"`

	// Questions carries structure on read and structure plus text on write.
	Questions []SurveyQuestion `json:"Questions"`

	// Labels is read only: language code -> label path -> text.
	Labels map[string]map[string]string `json:"Labels,omitempty"`

	// Write only.
	Valid        bool           `json:"Valid,omitempty"`
	Filter       map[string]any `json:"Filter,omitempty"`
	EnabledLangs []Language     `json:"EnabledLangs,omitempty"`
}

// Text resolves a step's text for one language, or "" when absent.
func (s Survey) Text(languageCode, questionKey, slot string) string {
	labels, ok := s.Labels[languageCode]
	if !ok {
		return ""
	}
	return labels[SurveyLabelPath(s.SurveyDefinitionID, questionKey, slot)]
}

// SurveyQuestion is one step of a survey.
//
// Key is assigned by the server and must be sent back on every update. An
// update that omits it does not fail — it silently appends a duplicate of the
// question, so a roster of four questions becomes eight. Order is the display
// position and is not a substitute for Key: after an update the two diverge.
type SurveyQuestion struct {
	// Read.
	Key           string  `json:"Key,omitempty"`
	Order         int     `json:"Order,omitempty"`
	Configuration *string `json:"Configuration,omitempty"`

	// Read and write.
	StepType int `json:"StepType"`

	// Write. Texts is keyed by numeric language id as a string.
	Texts         map[string]string    `json:"Texts,omitempty"`
	AnswerOptions []SurveyAnswerOption `json:"AnswerOptions,omitempty"`
	EditingLang   string               `json:"EditingLang,omitempty"`
	Valid         bool                 `json:"Valid,omitempty"`
}

// SurveyQuestionConfiguration is the decoded form of SurveyQuestion.Configuration.
type SurveyQuestionConfiguration struct {
	AnswerOptions []SurveyAnswerOption `json:"AnswerOptions"`
}

// DecodeConfiguration parses the Configuration field, which the API delivers as
// a JSON-encoded string rather than a nested object.
//
// Returns an empty configuration when the field is absent, which is the case for
// welcome, prompt and thankyou steps.
func (q SurveyQuestion) DecodeConfiguration() (SurveyQuestionConfiguration, error) {
	var config SurveyQuestionConfiguration
	if q.Configuration == nil || *q.Configuration == "" {
		return config, nil
	}

	if err := json.Unmarshal([]byte(*q.Configuration), &config); err != nil {
		return config, fmt.Errorf("wellbeing: decoding question %q configuration: %w", q.Key, err)
	}
	return config, nil
}

// SurveyAnswerOption is one selectable answer.
//
// LabelKey plays the same role for options that Key plays for questions: send it
// back on update or the option is duplicated rather than modified. Unlike
// questions, options can be removed by omitting them.
type SurveyAnswerOption struct {
	LabelKey string            `json:"LabelKey,omitempty"`
	Value    int               `json:"Value,omitempty"`
	Texts    map[string]string `json:"Texts,omitempty"`
	Editing  bool              `json:"Editing"`
}

// ListSurveys returns the company's surveys. Pass state zero for no filter.
//
// The list response leaves Questions null; use GetSurvey for the full object.
func (c *Client) ListSurveys(ctx context.Context, state int) ([]Survey, error) {
	query := url.Values{}
	query.Set("companyId", c.companyID)
	if state != 0 {
		query.Set("State", strconv.Itoa(state))
	}

	var out []Survey
	if _, err := c.do(ctx, http.MethodGet, "/v1.0/Survey?"+query.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetSurvey returns one survey including its questions and labels.
func (c *Client) GetSurvey(ctx context.Context, id int64) (*Survey, error) {
	var out Survey
	path := fmt.Sprintf("/v1.0/Survey/%d", id)
	if _, err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateSurvey creates a survey and returns its new id.
//
// The response body is a bare integer rather than an object.
func (c *Client) CreateSurvey(ctx context.Context, survey Survey) (int64, error) {
	survey.CompanyID = c.numericCompanyID()

	var id int64
	if _, err := c.do(ctx, http.MethodPost, "/v1.0/Survey", survey, &id); err != nil {
		return 0, err
	}
	return id, nil
}

// UpdateSurvey applies a full-object update.
//
// The API has no partial update: the complete question set must be present, and
// every existing question must carry its Key. Omitting Questions is rejected
// with an opaque 500, so that case is caught here instead.
func (c *Client) UpdateSurvey(ctx context.Context, survey Survey) error {
	if len(survey.Questions) == 0 {
		return errors.New("wellbeing: updating a survey requires the complete question set, " +
			"including the welcome and thankyou steps; the API rejects an empty one")
	}

	survey.CompanyID = c.numericCompanyID()

	_, err := c.do(ctx, http.MethodPost, "/v1.0/Survey/Update", survey, nil)
	return err
}

// ChangeSurveyState moves surveys to a lifecycle state.
//
// This is the closest the API offers to deleting a survey: there is no delete
// endpoint, so retiring one means moving it to StateInactive. Answers recorded
// against an inactive survey are retained.
func (c *Client) ChangeSurveyState(ctx context.Context, ids []int64, state int) error {
	if len(ids) == 0 {
		return nil
	}

	path := fmt.Sprintf("/v1.0/Survey/ChangeState/%d", state)
	_, err := c.do(ctx, http.MethodPost, path, ids, nil)
	return err
}

// numericCompanyID renders the configured company id for request bodies, which
// expect a number where the path expects a string.
func (c *Client) numericCompanyID() int64 {
	id, err := strconv.ParseInt(c.companyID, 10, 64)
	if err != nil {
		return 0
	}
	return id
}
