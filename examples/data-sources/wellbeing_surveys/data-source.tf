# Every survey in the company.
data "wellbeing_surveys" "all" {}

# Only the live ones.
data "wellbeing_surveys" "active" {
  state = "active"
}

output "active_survey_ids" {
  value = { for survey in data.wellbeing_surveys.active.surveys : survey.name => survey.id }
}
