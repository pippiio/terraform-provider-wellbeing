# The Wellbeing API replaces the whole roster on every write, so this single
# resource owns every employee created through the API. Employees omitted here
# are deleted by the API on the next apply.
resource "wellbeing_employee_roster" "this" {
  # Refuse to apply a change touching a quarter or more of the roster. Matches
  # the batch limit configured for the company in the Wellbeing portal.
  batch_limit_percent = 25

  employee = {
    for employee in local.hr_export : employee.staff_number => {
      firstname         = employee.first_name
      lastname          = employee.last_name
      email             = employee.email
      employment_status = employee.on_leave ? "on_leave" : "active"
      gender            = employee.gender
      phone_number      = employee.mobile

      dimensions = {
        Department       = employee.department
        ImmediateManager = employee.manager_name
        Location         = employee.office
      }
    }
  }

  timeouts {
    create = "60m"
    update = "60m"
  }
}
