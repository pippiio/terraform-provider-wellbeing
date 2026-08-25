terraform {
  required_providers {
    wellbeing = {
      source = "registry.terraform.io/techchapter/wellbeing"
    }
  }
}

provider "wellbeing" {
  host       = "https://api-ne1.worklifebarometer.com/"
  token      = var.wellbeing_token
  company_id = "1000"
}
