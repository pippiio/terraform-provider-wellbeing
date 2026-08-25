# Driving the roster from a company-wide people file. Because employee is a
# block rather than an attribute, generating entries needs `dynamic` rather than
# a plain `for` expression.
#
# users.yaml:
#   users:
#     mj@example.dk:
#       name: Mogens Jensen
#       phone: "+4512121212"
#       roles: [copenhagen, partner, employee, vpn]
#     mogens@example.dk:
#       name: Mogens Glistrup
#       phone: "+4513131313"
#       roles: [copenhagen, intern, employee, vpn, on_leave]

locals {
  users = yamldecode(file("${path.module}/users.yaml")).users

  # A flat roles list mixes location, job function and system access, so the
  # classification into Wellbeing dimension keys happens here rather than in the
  # provider — only you know which role means what.
  locations = ["copenhagen", "aarhus"]
  job_roles = ["partner", "manager", "consultant", "apprentice", "intern"]
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

      dimensions = {
        Location = one([for role in employee.value.roles : role if contains(local.locations, role)])
        Role     = one([for role in employee.value.roles : role if contains(local.job_roles, role)])
      }
    }
  }
}
