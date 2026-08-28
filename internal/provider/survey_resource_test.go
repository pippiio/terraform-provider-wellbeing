package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

// fakeSurveyAPI is an in-memory stand-in for the survey endpoints.
//
// It reproduces the behaviour that shapes the whole design: a question sent
// without its Key is appended rather than updated. Without that, the acceptance
// tests would pass against a fake that is kinder than the real API.
type fakeSurveyAPI struct {
	mu        sync.Mutex
	surveys   map[int64]*wellbeingclient.Survey
	nextID    int64
	nextDefID int64
	creates   int
	updates   int
}

func newFakeSurveyAPI() *fakeSurveyAPI {
	return &fakeSurveyAPI{
		surveys:   map[int64]*wellbeingclient.Survey{},
		nextID:    1700,
		nextDefID: 1500,
	}
}

func (f *fakeSurveyAPI) questionCount(id int64) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	survey, ok := f.surveys[id]
	if !ok {
		return -1
	}
	return len(survey.Questions)
}

func (f *fakeSurveyAPI) only() *wellbeingclient.Survey {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, survey := range f.surveys {
		return survey
	}
	return nil
}

// store folds an incoming payload into the stored survey, mimicking the API:
// keyed questions replace their counterpart, unkeyed ones are appended, and text
// moves into the Labels map keyed by language code.
func (f *fakeSurveyAPI) store(survey *wellbeingclient.Survey, incoming wellbeingclient.Survey) {
	codeByID := map[string]string{"1041": "da", "1045": "en", "1058": "de"}

	survey.Name = incoming.Name
	survey.StartDate = incoming.StartDate
	survey.EndDate = incoming.EndDate
	survey.Frequency = incoming.Frequency
	survey.State = incoming.State
	if survey.Labels == nil {
		survey.Labels = map[string]map[string]string{}
	}

	byKey := map[string]int{}
	for i, existing := range survey.Questions {
		byKey[existing.Key] = i
	}

	for _, question := range incoming.Questions {
		index, existing := byKey[question.Key]
		if question.Key == "" || !existing {
			key := fmt.Sprintf("Q%d", len(survey.Questions)+1)
			survey.Questions = append(survey.Questions, wellbeingclient.SurveyQuestion{
				Key: key, Order: len(survey.Questions) + 1, StepType: question.StepType,
			})
			index = len(survey.Questions) - 1
			byKey[key] = index
		}
		stored := &survey.Questions[index]
		stored.StepType = question.StepType

		slot := wellbeingclient.SurveySlotQuestion
		if question.StepType == wellbeingclient.StepTypeWelcome || question.StepType == wellbeingclient.StepTypeThankYou {
			slot = wellbeingclient.SurveySlotContent
		}
		for id, text := range question.Texts {
			code := codeByID[id]
			if survey.Labels[code] == nil {
				survey.Labels[code] = map[string]string{}
			}
			survey.Labels[code][wellbeingclient.SurveyLabelPath(survey.SurveyDefinitionID, stored.Key, slot)] = text
		}

		if len(question.AnswerOptions) > 0 {
			config := wellbeingclient.SurveyQuestionConfiguration{}
			for i, option := range question.AnswerOptions {
				labelKey := option.LabelKey
				if labelKey == "" {
					labelKey = wellbeingclient.SurveySlotOption(i + 1)
				}
				config.AnswerOptions = append(config.AnswerOptions,
					wellbeingclient.SurveyAnswerOption{LabelKey: labelKey, Value: i + 1})
				for id, text := range option.Texts {
					code := codeByID[id]
					if survey.Labels[code] == nil {
						survey.Labels[code] = map[string]string{}
					}
					survey.Labels[code][wellbeingclient.SurveyLabelPath(survey.SurveyDefinitionID, stored.Key, labelKey)] = text
				}
			}
			encoded, _ := json.Marshal(config)
			raw := string(encoded)
			stored.Configuration = &raw
		}
	}
}

func (f *fakeSurveyAPI) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case path == "/v1.0/Company/1296/Language/Enabled":
			_, _ = w.Write([]byte(`[{"id":1041,"label":"Danish","code":"da"},{"id":1045,"label":"English","code":"en"}]`))

		case path == "/v1.0/Survey" && r.Method == http.MethodGet:
			out := make([]wellbeingclient.Survey, 0, len(f.surveys))
			for _, survey := range f.surveys {
				summary := *survey
				summary.Questions = nil
				summary.Labels = nil
				out = append(out, summary)
			}
			_ = json.NewEncoder(w).Encode(out)

		case path == "/v1.0/Survey" && r.Method == http.MethodPost:
			var incoming wellbeingclient.Survey
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`malformed payload`))
				return
			}
			// Mirror the real validation: a survey needs both pages.
			if !hasStep(incoming, wellbeingclient.StepTypeWelcome) || !hasStep(incoming, wellbeingclient.StepTypeThankYou) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`Survey must contain one cover page and one thank you page`))
				return
			}
			f.creates++
			f.nextID++
			f.nextDefID++
			survey := &wellbeingclient.Survey{ID: f.nextID, SurveyDefinitionID: f.nextDefID}
			f.store(survey, incoming)
			f.surveys[survey.ID] = survey
			_, _ = w.Write([]byte(fmt.Sprintf("%d", survey.ID)))

		case path == "/v1.0/Survey/Update":
			var incoming wellbeingclient.Survey
			_ = json.NewDecoder(r.Body).Decode(&incoming)
			if len(incoming.Questions) == 0 {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`Survey must contain one cover page and one thank you page`))
				return
			}
			survey, ok := f.surveys[incoming.ID]
			if !ok {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`Not found`))
				return
			}
			f.updates++
			f.store(survey, incoming)
			w.WriteHeader(http.StatusOK)

		case strings.HasPrefix(path, "/v1.0/Survey/ChangeState/"):
			state := path[len("/v1.0/Survey/ChangeState/"):]
			var ids []int64
			_ = json.NewDecoder(r.Body).Decode(&ids)
			for _, id := range ids {
				if survey, ok := f.surveys[id]; ok {
					switch state {
					case "1":
						survey.State = wellbeingclient.StateDraft
					case "2":
						survey.State = wellbeingclient.StateActive
					case "3":
						survey.State = wellbeingclient.StateInactive
					}
				}
			}
			w.WriteHeader(http.StatusOK)

		case strings.HasPrefix(path, "/v1.0/Survey/"):
			var id int64
			_, _ = fmt.Sscanf(path, "/v1.0/Survey/%d", &id)
			survey, ok := f.surveys[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`Not found`))
				return
			}
			detail := *survey
			detail.CompanyID = 0 // the real detail response omits it
			_ = json.NewEncoder(w).Encode(detail)

		default:
			http.NotFound(w, r)
		}
	})
}

func hasStep(survey wellbeingclient.Survey, stepType int) bool {
	for _, question := range survey.Questions {
		if question.StepType == stepType {
			return true
		}
	}
	return false
}

func surveyProviderConfig(host string) string {
	return fmt.Sprintf(`
provider "wellbeing" {
  host       = %q
  token      = "test-token"
  company_id = "1296"
}
`, host)
}

const baseSurvey = `
resource "wellbeing_survey" "this" {
  name             = "Trivsel"
  default_language = "da"
  first_page       = "Velkommen"
  last_page        = "Tak for svarene"
  frequency        = "quarterly"
  start            = "2030-01-01T00:00:00Z"
  end              = "2030-12-31T00:00:00Z"

  question {
    type = "option"
    text = "Er du tilfreds?"
    answer { text = "Ja" }
    answer { text = "Nej" }
  }

  question {
    type = "prompt"
    text = "Uddyb gerne"
  }
}
`

func TestAccSurveyCreatesWithPagesAndQuestions(t *testing.T) {
	skipWithoutTerraform(t)

	fake := newFakeSurveyAPI()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: surveyProviderConfig(srv.URL) + baseSurvey,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("wellbeing_survey.this", "id"),
					resource.TestCheckResourceAttrSet("wellbeing_survey.this", "definition_id"),
					resource.TestCheckResourceAttr("wellbeing_survey.this", "question.#", "2"),
					// The key is derived from the question text.
					resource.TestCheckResourceAttr("wellbeing_survey.this", "question.0.key", "er-du-tilfreds"),
					// The server identity is recorded so updates modify rather than duplicate.
					resource.TestCheckResourceAttrSet("wellbeing_survey.this", "question.0.server_key"),
					resource.TestCheckResourceAttr("wellbeing_survey.this", "question.0.answer.#", "2"),
					resource.TestCheckResourceAttrSet("wellbeing_survey.this", "question.0.answer.0.server_label_key"),
					// state defaults to draft, so a survey never reaches employees by accident.
					resource.TestCheckResourceAttr("wellbeing_survey.this", "state", "draft"),
				),
			},
		},
	})

	// The stored survey holds the two questions plus both pages.
	if got := fake.questionCount(fake.only().ID); got != 4 {
		t.Errorf("stored questions = %d, want 4 (welcome + 2 + thankyou)", got)
	}
}

func TestAccSurveyRepeatedApplyDoesNotDuplicateQuestions(t *testing.T) {
	// The guard for the highest-scoring risk. An update that omits the server
	// key appends a copy of every question instead of modifying it, and the API
	// answers 200 either way — so only the question count reveals the bug.
	skipWithoutTerraform(t)

	fake := newFakeSurveyAPI()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	config := surveyProviderConfig(srv.URL) + baseSurvey
	scheduled := strings.Replace(config, `end              = "2030-12-31T00:00:00Z"`, `end              = "2031-06-30T00:00:00Z"`, 1)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				// A scheduling-only change: updates in place, must not replace.
				Config: scheduled,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("wellbeing_survey.this", "end", "2031-06-30T00:00:00Z"),
					resource.TestCheckResourceAttr("wellbeing_survey.this", "question.#", "2"),
				),
			},
			{
				// Applying the same config again must be a no-op.
				Config:   scheduled,
				PlanOnly: true,
			},
		},
	})

	if got := fake.questionCount(fake.only().ID); got != 4 {
		t.Errorf("stored questions = %d after an update, want 4 — the questions were duplicated", got)
	}
	if fake.creates != 1 {
		t.Errorf("creates = %d, want 1 — a scheduling change must not replace the survey", fake.creates)
	}
}

func TestAccSurveyContentChangeForcesReplacement(t *testing.T) {
	skipWithoutTerraform(t)

	fake := newFakeSurveyAPI()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	edited := strings.Replace(baseSurvey, `text = "Uddyb gerne"`, `text = "Uddyb gerne her"`, 1)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: surveyProviderConfig(srv.URL) + baseSurvey},
			{
				Config: surveyProviderConfig(srv.URL) + edited,
				// Asserted inside the step: the framework destroys everything
				// once the case finishes, which would deactivate both surveys
				// and make a post-hoc check meaningless.
				Check: func(*terraform.State) error {
					total, inactive := fake.census()
					if total != 2 {
						return fmt.Errorf("stored surveys = %d, want 2 — the old survey must be retained", total)
					}
					if inactive != 1 {
						return fmt.Errorf("inactive surveys = %d, want 1 — the replaced survey must be deactivated", inactive)
					}
					return nil
				},
			},
		},
	})

	if fake.creates != 2 {
		t.Errorf("creates = %d, want 2 — editing a question must create a new survey", fake.creates)
	}
}

// census reports how many surveys are stored and how many are inactive.
func (f *fakeSurveyAPI) census() (total, inactive int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, survey := range f.surveys {
		total++
		if survey.State == wellbeingclient.StateInactive {
			inactive++
		}
	}
	return total, inactive
}

func TestAccSurveyRejectsAnswersOnAPromptQuestion(t *testing.T) {
	skipWithoutTerraform(t)

	fake := newFakeSurveyAPI()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: surveyProviderConfig(srv.URL) + `
resource "wellbeing_survey" "this" {
  name             = "Trivsel"
  default_language = "da"
  first_page       = "Velkommen"
  last_page        = "Tak"
  frequency        = "quarterly"
  start            = "2030-01-01T00:00:00Z"
  end              = "2030-12-31T00:00:00Z"

  question {
    type = "prompt"
    text = "Uddyb gerne"
    answer { text = "Ja" }
  }
}
`,
				ExpectError: regexp.MustCompile(`Answer blocks on a free-text question`),
			},
		},
	})
}

func TestAccSurveyRejectsDuplicateDerivedKeys(t *testing.T) {
	skipWithoutTerraform(t)

	fake := newFakeSurveyAPI()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: surveyProviderConfig(srv.URL) + `
resource "wellbeing_survey" "this" {
  name             = "Trivsel"
  default_language = "da"
  first_page       = "Velkommen"
  last_page        = "Tak"
  frequency        = "quarterly"
  start            = "2030-01-01T00:00:00Z"
  end              = "2030-12-31T00:00:00Z"

  question {
    type = "prompt"
    text = "Er du tilfreds?"
  }

  question {
    type = "prompt"
    text = "Er du tilfreds?"
  }
}
`,
				ExpectError: regexp.MustCompile(`Duplicate question key`),
			},
		},
	})
}

func TestAccSurveyRejectsUnknownFrequency(t *testing.T) {
	skipWithoutTerraform(t)

	fake := newFakeSurveyAPI()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      surveyProviderConfig(srv.URL) + strings.Replace(baseSurvey, `"quarterly"`, `"fortnightly"`, 1),
				ExpectError: regexp.MustCompile(`Invalid Attribute Value Match`),
			},
		},
	})
}

func TestAccSurveyMultiLanguageRoundTrips(t *testing.T) {
	skipWithoutTerraform(t)

	fake := newFakeSurveyAPI()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: surveyProviderConfig(srv.URL) + `
resource "wellbeing_survey" "this" {
  name             = "Trivsel"
  default_language = "da"
  first_page       = "Velkommen"
  first_page_texts = { en = "Welcome" }
  last_page        = "Tak"
  frequency        = "quarterly"
  start            = "2030-01-01T00:00:00Z"
  end              = "2030-12-31T00:00:00Z"

  question {
    type  = "option"
    text  = "Er du tilfreds?"
    texts = { en = "Are you satisfied?" }
    answer {
      text  = "Ja"
      texts = { en = "Yes" }
    }
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("wellbeing_survey.this", "question.0.texts.en", "Are you satisfied?"),
					resource.TestCheckResourceAttr("wellbeing_survey.this", "question.0.answer.0.texts.en", "Yes"),
					resource.TestCheckResourceAttr("wellbeing_survey.this", "first_page_texts.en", "Welcome"),
				),
			},
		},
	})
}

func TestAccSurveyRejectsUnenabledLanguage(t *testing.T) {
	skipWithoutTerraform(t)

	fake := newFakeSurveyAPI()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: surveyProviderConfig(srv.URL) + strings.Replace(baseSurvey,
					`text = "Uddyb gerne"`, "text = \"Uddyb gerne\"\n    texts = { fr = \"Expliquez\" }", 1),
				ExpectError: regexp.MustCompile(`Unknown language code`),
			},
		},
	})
}

func TestAccSurveysDataSourceListsSurveys(t *testing.T) {
	skipWithoutTerraform(t)

	fake := newFakeSurveyAPI()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: surveyProviderConfig(srv.URL) + baseSurvey + `
data "wellbeing_surveys" "all" {
  depends_on = [wellbeing_survey.this]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.wellbeing_surveys.all", "surveys.#", "1"),
					resource.TestCheckResourceAttr("data.wellbeing_surveys.all", "surveys.0.name", "Trivsel"),
					resource.TestCheckResourceAttr("data.wellbeing_surveys.all", "surveys.0.state", "draft"),
				),
			},
		},
	})
}
