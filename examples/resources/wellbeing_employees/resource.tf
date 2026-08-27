# The Wellbeing API replaces the whole roster on every write, so this single
# resource owns every employee created through the API. Anyone omitted here is
# deleted by the API on the next apply.
resource "wellbeing_employees" "this" {
  # Prefixed to any phone that does not already start with +, so employees can
  # carry national numbers while the API receives the international form.
  default_country_code = "+45"

  # batch_limit_percent defaults to 25, matching the value the Wellbeing
  # documentation recommends configuring in the portal: an apply touching a
  # quarter or more of the roster fails before anything is sent. Raise it if the
  # company's configured limit is higher, or set it to 0 to disable the check.

  employee {
    id    = "mj"
    name  = "Mogens Jensen"
    email = "mj@example.dk"
    phone = "12121212"

    # Dimension keys are what Wellbeing reports group by and what survey
    # selection rules filter on.
    dimensions = {
      Location = "copenhagen"
    }
  }

  employee {
    id     = "mogens"
    name   = "Mogens Glistrup"
    email  = "mogens@example.dk"
    phone  = "+4513131313"
    active = false # on leave
    dimensions = {
      Location = "copenhagen"
    }
  }

  # name is split at the first space, so a single-word name needs an explicit
  # lastname. The same override fixes any name the splitter gets wrong.
  employee {
    id       = "rds"
    name     = "Rosario"
    lastname = "de Silva"
    email    = "rds@example.dk"
  }


  timeouts {
    create = "60m"
    update = "60m"
  }
}
