# Driving the roster from a company-wide people file. Because employee is a
# block rather than an attribute, generating entries needs `dynamic` rather than
# a plain `for` expression.
#
# users.yaml:
#   users:
#     mj@example.dk:
#       name: Mogens Jensen
#       phone: "+4512121212"
#       roles: [copenhagen]
#     rds@example.dk:
#       name: Rosario de Silva
#       phone: "+4513131313"
#       roles: [odense, on_leave]
#     ci@example.dk:
#       name: CI Robot
#       roles: [employee, vpn]

locals {
  users = yamldecode(file("${path.module}/users.yaml")).users

  # Which roles feed which Wellbeing dimension.
  #
  # A flat roles list mixes location, job function and system access, and only
  # you know which is which — the provider cannot infer that copenhagen is a
  # Location. Declare the candidates for each dimension once here.
  #
  # Order is precedence: the first candidate an employee holds wins. Mogens is
  # both a consultant and an intern, and is reported as a consultant because
  # consultant is listed first.
  dimension_candidates = {
    Location = ["copenhagen", "odense", "aarhus"]
    Role     = ["partner", "manager", "consultant", "apprentice", "intern"]
  }

  # The matched value per dimension, per user. Dimensions the user holds no
  # candidate role for are dropped rather than set empty, so nobody is filed
  # under a blank department.
  user_dimensions = {
    for email, user in local.users : email => {
      for dimension, candidates in local.dimension_candidates :
      dimension => [for candidate in candidates : candidate if contains(user.roles, candidate)][0]
      if length([for candidate in candidates : candidate if contains(user.roles, candidate)]) > 0
    }
  }
}

resource "wellbeing_employees" "from_yaml" {
  default_country_code = "+45"

  dynamic "employee" {
    for_each = local.users

    content {
      id     = employee.key
      email  = employee.key
      name   = employee.value.name
      phone  = try(employee.value.phone, null)
      active = !contains(employee.value.roles, "on_leave")

      # jr  -> { Location = "copenhagen", Role = "partner" }
      # anne-> { Location = "odense",     Role = "consultant" }
      # ci  -> { }  (no location or job role among its roles)
      dimensions = local.user_dimensions[employee.key]
    }
  }
}
