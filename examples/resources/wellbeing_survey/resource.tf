# A survey is immutable in its content. Changing the name, either page, or any
# question or answer replaces it: a new survey is created and the old one is
# deactivated, keeping its answers.
#
# That is deliberate. Editing a running survey would leave answers already given
# attached to a question that no longer asks what it asked at the time, and the
# provider cannot tell a corrected typo from a changed meaning.
resource "wellbeing_survey" "enps" {
  name             = "Employee Net Promoter Score"
  default_language = "da"

  # Both pages are required — the API rejects a survey without them.
  first_page = "Svar venligst på følgende spørgsmål om din arbejdsplads."
  last_page  = "Tak for svarene"

  frequency = "quarterly"
  start     = "2030-01-01T00:00:00Z"
  end       = "2030-12-31T00:00:00Z"

  # Surveys start as drafts. Set this to "active" to send it to employees.
  state = "draft"

  question {
    type = "option"
    text = "I hvor høj grad vil du anbefale virksomheden som arbejdsplads?"

    answer { text = "I meget høj grad" }
    answer { text = "I nogen grad" }
    answer { text = "I mindre grad" }
    answer { text = "Slet ikke" }
  }

  question {
    type = "prompt"
    text = "Uddyb gerne dit svar"
  }

  # A replacement creates the new survey before deactivating the old one, so
  # there is never a window with no live survey.
  lifecycle {
    create_before_destroy = true
  }
}

# Multiple languages: the plain attributes carry default_language, and the
# _texts maps carry the rest, keyed by language code.
resource "wellbeing_survey" "workplace" {
  name             = "Workplace"
  default_language = "da"

  first_page       = "Velkommen"
  first_page_texts = { en = "Welcome" }
  last_page        = "Tak for svarene"
  last_page_texts  = { en = "Thanks for your answers" }

  frequency = "quarterly"
  start     = "2030-01-01T00:00:00Z"
  end       = "2030-12-31T00:00:00Z"

  question {
    # Two questions worded identically would derive the same key and fail at
    # plan time. Set the key explicitly to disambiguate — and to keep a stable
    # handle if you ever reword the question.
    key   = "jargon"
    type  = "option"
    text  = "Føler du dig tilpas i tonen på arbejdspladsen?"
    texts = { en = "Are you comfortable with the tone at work?" }

    answer {
      text  = "Ja"
      texts = { en = "Yes" }
    }
    answer {
      text  = "Nej"
      texts = { en = "No" }
    }
  }
}
