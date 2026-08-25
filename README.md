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
resource "wellbeing_employees" "this" {
  default_country_code = "+45"

  dynamic "employee" {
    for_each = yamldecode(file("users.yaml")).users

    content {
      id     = employee.key
      email  = employee.key
      name   = employee.value.name
      phone  = try(employee.value.phone, null)
      active = !contains(employee.value.roles, "on_leave")

      dimensions = {
        Location = one([for r in employee.value.roles : r if contains(local.locations, r)])
        Role     = one([for r in employee.value.roles : r if contains(local.job_roles, r)])
      }
    }
  }
}
```

Wellbeing dimensions are key-value pairs, and the keys drive reporting and survey selection. A flat roles list mixing location, job function and system access has to be classified into those keys in HCL — the provider cannot tell which role means what.

Generate a token in the Wellbeing portal under *Company* > *Integration* (turn on External Integration), then *Access control* > `<YourCompanyName> API User` > *Generate new api token*. The dialog also shows your company ID.

## How the roster works

The Wellbeing API has no per-employee endpoint. `PUT /Employee` replaces the entire set, and employees absent from the payload are deleted. The provider therefore models the workforce as one `wellbeing_employees` resource rather than one resource per person — a per-employee resource would delete everyone else on every create.

Writes may be queued: the API returns `202 Accepted` and processes the import asynchronously, usually within minutes but occasionally up to an hour. The provider polls until the operation settles, so a successful `terraform apply` means the change actually landed. Tune the wait with a `timeouts` block.

Large changes are guarded. `batch_limit_percent` defaults to `25`, the value the API documentation recommends configuring as the company's batch limit, and an apply touching that share of the roster or more fails before any request is sent. The API enforces the same rule server-side but never exposes the configured number, and it may reject an import only after a long queued wait. The check is skipped below 100 employees, matching the API, so a first import into an empty company is never blocked. Set it to `0` to turn the guard off.

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
