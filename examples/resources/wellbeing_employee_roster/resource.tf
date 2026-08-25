# The Wellbeing API replaces the whole roster on every write, so this single
# resource owns every employee created through the API. Employees omitted here
# are deleted by the API on the next apply.
resource "wellbeing_employee_roster" "this" {
  # batch_limit_percent defaults to 25, matching the value the Wellbeing
  # documentation recommends configuring in the portal: an apply touching a
  # quarter or more of the roster fails before anything is sent. Raise it if the
  # company's configured limit is higher, or set it to 0 to disable the check.

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
