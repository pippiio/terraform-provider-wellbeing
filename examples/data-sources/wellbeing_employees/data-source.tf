# Returns only employees created through the API; employees created in the
# Wellbeing portal are not included.
data "wellbeing_employees" "this" {}

output "locked_employees" {
  value = [
    for employee_id, employee in data.wellbeing_employees.this.employees :
    employee_id if employee.locked
  ]
}
