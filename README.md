# terraform-provider-wellbeing

Terraform provider for [HR.ON Wellbeing](https://dashboard.worklifebarometer.com/) (previously howdy.care).

## Usage

```terraform
provider "wellbeing" {
  token      = var.wellbeing_token   # or WELLBEING_TOKEN
  company_id = var.wellbeing_company # or WELLBEING_COMPANY_ID
}

resource "wellbeing_employees" "this" {
  default_country_code = "+45"

  employee {
    id    = "mj"
    name  = "Mogens Jensen"
    email = "mj@example.dk"
    phone = "12121212"

    dimensions = {
      Location = "copenhagen"
      Role     = "partner"
    }
  }

  employee {
    id     = "mogens"
    name   = "Mogens Glistrup"
    email  = "mogens@example.dk"
    active = false # on leave
  }
}
```

`name` is split at the first space, so `"Anna Van der Berg"` becomes `Anna` / `Van der Berg`. Set `firstname` or `lastname` to override a split it gets wrong; a single-word name has no surname, which the API rejects, so those need an explicit `lastname`.

`default_country_code` completes any `phone` that does not already start with `+`. Digits are not otherwise rewritten — a national trunk prefix is left alone, because stripping it correctly depends on the country.

### Driving it from a people file

`employee` is a block, so entries are generated with `dynamic` rather than a `for` expression:

```terraform
locals {
  users = yamldecode(file("users.yaml")).users

  # Which roles feed which Wellbeing dimension. Order is precedence: the first
  # candidate an employee holds wins, so someone who is both consultant and
  # intern is reported as consultant.
  dimension_candidates = {
    Location = ["copenhagen", "odense", "aarhus"]
    Role     = ["partner", "manager", "consultant", "apprentice", "intern"]
  }

  # The matched value per dimension, per user. Dimensions the user holds no
  # candidate role for are dropped rather than set empty.
  user_dimensions = {
    for email, user in local.users : email => {
      for dimension, candidates in local.dimension_candidates :
      dimension => [for candidate in candidates : candidate if contains(user.roles, candidate)][0]
      if length([for candidate in candidates : candidate if contains(user.roles, candidate)]) > 0
    }
  }
}

resource "wellbeing_employees" "this" {
  default_country_code = "+45"

  dynamic "employee" {
    for_each = local.users

    content {
      id     = employee.key
      email  = employee.key
      name   = employee.value.name
      phone  = try(employee.value.phone, null)
      active = !contains(employee.value.roles, "on_leave")

      dimensions = local.user_dimensions[employee.key]
    }
  }
}
```

Given `roles: [copenhagen, partner, employee, vpn]` that yields `{ Location = "copenhagen", Role = "partner" }`.

Wellbeing dimensions are key-value pairs, and the keys drive reporting and survey selection. A flat roles list mixing location, job function and system access has to be classified into those keys in HCL — the provider cannot tell that `copenhagen` is a Location and `vpn` is neither.

**Destroying the resource does not delete anyone.** The API has no delete operation, so `terraform destroy` removes the roster from state and leaves Wellbeing untouched, with a warning saying so. Use `terraform import wellbeing_employees.this <company_id>` to adopt the roster back into state.

> [!WARNING]
> `GET /Employee` returns only employees created through the API, while `PUT /Employee` is documented as "a complete set based change of all employees in the system". Whether a `PUT` also deletes employees created in the Wellbeing portal is unverified. Test against the UAT environment (`https://wlb-uat-ne1-api.azurewebsites.net/`, reset daily from production) before using this provider against production data.

## Development

```bash
go build ./...
go test ./...
go generate ./tools    # regenerate docs/
```

Tests that drive a real plan/apply cycle need the `terraform` binary on `PATH`; they skip automatically when it is missing. No test contacts the real Wellbeing API.
