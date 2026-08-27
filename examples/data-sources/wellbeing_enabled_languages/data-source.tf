data "wellbeing_enabled_languages" "this" {}

output "language_ids_by_code" {
  value = {
    for language in data.wellbeing_enabled_languages.this.languages :
    language.code => language.id
  }
}
