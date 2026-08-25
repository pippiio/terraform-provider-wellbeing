terraform {
  required_providers {
    wellbeing = {
      source = "registry.terraform.io/techchapter/wellbeing"
    }
  }
}

# host defaults to https://api-ne1.worklifebarometer.com/
# Every attribute may also be supplied via the environment:
#   WELLBEING_HOST, WELLBEING_TOKEN, WELLBEING_COMPANY_ID
provider "wellbeing" {
  token      = var.wellbeing_token
  company_id = var.wellbeing_company_id
}
