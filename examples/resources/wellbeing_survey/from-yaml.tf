# Driving surveys from YAML files, the way the pre-provider setup did.
#
# Pure HCL is the recommended path — it validates at plan time and reads better
# in a diff. This pattern exists for rosters of surveys already maintained as
# data, and mirrors the employee from-yaml example.
locals {
  surveys = { for path in fileset(path.module, "surveys/*.yaml") :
    trimsuffix(basename(path), ".yaml") => yamldecode(file(path))
  }
}

resource "wellbeing_survey" "from_yaml" {
  for_each = local.surveys

  name             = each.value.name
  default_language = "da"

  first_page = each.value.firstPage
  last_page  = each.value.lastPage

  frequency = lower(each.value.frequency)
  start     = each.value.start
  end       = each.value.end

  dynamic "question" {
    for_each = each.value.questions
    content {
      # Questions repeated verbatim within one survey would derive the same key,
      # so the index disambiguates them. Keys must stay stable: changing one is
      # a content change and replaces the survey.
      key  = "q${question.key + 1}"
      type = question.value.type
      text = question.value.question

      dynamic "answer" {
        for_each = try(question.value.answers, [])
        content {
          text = answer.value
        }
      }
    }
  }

  lifecycle {
    create_before_destroy = true
  }
}
