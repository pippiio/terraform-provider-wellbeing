# The Wellbeing API has no single company endpoint, so this composes the
# endpoints that do exist: the configured company ID, a roster count, and the
# enabled languages.
data "wellbeing_company" "this" {}

output "company_id" {
  value = data.wellbeing_company.this.id
}

# Counts only employees created through the API; employees created in the
# Wellbeing portal are not returned by the API.
output "employee_count" {
  value = data.wellbeing_company.this.employee_count
}

output "language_ids_by_code" {
  value = {
    for language in data.wellbeing_company.this.enabled_languages :
    language.code => language.id
  }
}
