package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"regexp"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

// skipWithoutTerraform skips tests that need the Terraform CLI to drive a real
// plan/apply cycle. It runs the binary rather than only looking it up on PATH,
// because version managers install shims that resolve but fail to execute when
// no version is selected.
func skipWithoutTerraform(t *testing.T) {
	t.Helper()

	path, err := exec.LookPath("terraform")
	if err != nil {
		t.Skip("terraform binary not found on PATH; skipping plan/apply test")
	}
	if out, err := exec.Command(path, "version").CombinedOutput(); err != nil {
		t.Skipf("terraform on PATH is not runnable (%v): %s", err, out)
	}
}

// fakeRoster is an in-memory stand-in for the Wellbeing employee API.
type fakeRoster struct {
	mu        sync.Mutex
	employees []wellbeingclient.Employee
	puts      int
	lastPut   []wellbeingclient.Employee
}

func (f *fakeRoster) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		if r.URL.Path != "/v1.0/Company/1000/Employee" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(f.employees)

		case http.MethodPut:
			var incoming []wellbeingclient.Employee
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"ValidationErrors":["malformed payload"]}`))
				return
			}
			f.puts++
			f.lastPut = incoming

			// Mimic the server assigning read-only fields, including nesting the
			// hoisted department fields back into Dimensions.
			stored := make([]wellbeingclient.Employee, 0, len(incoming))
			for i, e := range incoming {
				e.ID = int64(1000 + i)
				e.CompanyID = 1000
				e.Fullname = e.Firstname + " " + e.Lastname
				e.ExternalID = e.EmployeeID
				e.SourcedFromExternalSystem = true
				e.ContactNumber = e.Phonenumber

				if e.Department != nil {
					if e.Dimensions == nil {
						e.Dimensions = map[string]string{}
					}
					e.Dimensions["Department"] = *e.Department
					e.Department = nil
				}
				e.Phonenumber = nil
				e.InvitationDate = nil

				stored = append(stored, e)
			}
			f.employees = stored

			_, _ = w.Write([]byte(`{"ApiOperationId":"op-1","Inserted":1,"Updated":0,"Removed":0,"WasQueued":"false"}`))

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

// seedRoster pre-populates the fake API so a test can exercise a change against
// an existing headcount. The batch limit only applies at 100 employees or more.
func seedRoster(fake *fakeRoster, count int) {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	fake.employees = make([]wellbeingclient.Employee, 0, count)
	for i := range count {
		fake.employees = append(fake.employees, wellbeingclient.Employee{
			EmployeeID:       fmt.Sprintf("seed-%d", i),
			Firstname:        "Seed",
			Lastname:         fmt.Sprintf("Number%d", i),
			Email:            fmt.Sprintf("seed%d@example.dk", i),
			EmploymentStatus: 0,
		})
	}
}

func fakeProviderConfig(host string) string {
	return fmt.Sprintf(`
provider "wellbeing" {
  host       = %q
  token      = "test-token"
  company_id = "1000"
}
`, host)
}

func TestAccEmployeesCreateAndUpdate(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(srv.URL) + `
resource "wellbeing_employees" "this" {
  default_country_code = "+45"

  employee {
    id    = "mj"
    name  = "Mogens Jensen"
    email = "mj@example.dk"
    phone = "12121212"

    dimensions = {
      Location = "copenhagen"
      Role     = "consultant"
    }
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.#", "1"),
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.0.id", "mj"),
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.0.name", "Mogens Jensen"),
					// active defaults to true.
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.0.active", "true"),
					// The name was split for the API and echoed back as fullname.
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.0.fullname", "Mogens Jensen"),
					// The bare phone stays as configured, not as normalised.
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.0.phone", "12121212"),
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.0.dimensions.Role", "consultant"),
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.0.dimensions.Location", "copenhagen"),
					resource.TestCheckResourceAttr("wellbeing_employees.this", "id", "1000"),
					// batch_limit_percent keeps its documented default.
					resource.TestCheckResourceAttr("wellbeing_employees.this", "batch_limit_percent", "25"),
				),
			},
			{
				// A second employee on leave, plus an explicit lastname override.
				Config: fakeProviderConfig(srv.URL) + `
resource "wellbeing_employees" "this" {
  default_country_code = "+45"

  employee {
    id    = "mj"
    name  = "Mogens Jensen"
    email = "mj@example.dk"
    phone = "12121212"

    dimensions = {
      Location = "copenhagen"
      Role     = "consultant"
    }
  }

  employee {
    id     = "mogens"
    name   = "Mogens Glistrup"
    email  = "mogens@example.dk"
    phone  = "+4513131313"
    active = false
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.#", "2"),
					// Block order is preserved across the refresh.
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.0.id", "mj"),
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.1.id", "mogens"),
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.1.active", "false"),
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.1.fullname", "Mogens Glistrup"),
				),
			},
		},
	})
}

func TestAccEmployeesPrependsDefaultCountryCode(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(srv.URL) + `
resource "wellbeing_employees" "this" {
  default_country_code = "+45"

  employee {
    id    = "mj"
    name  = "Mogens Jensen"
    email = "mj@example.dk"
    phone = "12121212"
  }

  employee {
    id    = "abroad"
    name  = "Erika Mustermann"
    email = "erika@example.dk"
    phone = "+4915112345678"
  }
}
`,
			},
		},
	})

	fake.mu.Lock()
	defer fake.mu.Unlock()

	sent := map[string]string{}
	for _, e := range fake.lastPut {
		if e.Phonenumber != nil {
			sent[e.EmployeeID] = *e.Phonenumber
		}
	}

	if got := sent["mj"]; got != "+4512121212" {
		t.Errorf("jr phone sent as %q, want the bare 12121212 prefixed with +45", got)
	}
	if got := sent["abroad"]; got != "+4915112345678" {
		t.Errorf("abroad phone sent as %q, want its own country code preserved", got)
	}
}

func TestAccEmployeesRejectsSingleWordName(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(srv.URL) + `
resource "wellbeing_employees" "this" {
  employee {
    id    = "rds"
    name  = "Rosario"
    email = "rds@example.dk"
  }
}
`,
				ExpectError: regexp.MustCompile(`(?s)missing a last name`),
			},
		},
	})
}

func TestAccEmployeesLastnameOverrideRescuesSingleWordName(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(srv.URL) + `
resource "wellbeing_employees" "this" {
  employee {
    id       = "rds"
    name     = "Rosario"
    lastname = "de Silva"
    email    = "rds@example.dk"
  }
}
`,
				Check: resource.TestCheckResourceAttr(
					"wellbeing_employees.this", "employee.0.fullname", "Rosario de Silva"),
			},
		},
	})
}

func TestAccEmployeesRejectsBarePhoneWithoutCountryCode(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(srv.URL) + `
resource "wellbeing_employees" "this" {
  employee {
    id    = "mj"
    name  = "Mogens Jensen"
    email = "mj@example.dk"
    phone = "12121212"
  }
}
`,
				ExpectError: regexp.MustCompile(`(?s)no country code`),
			},
		},
	})
}

func TestAccEmployeesRejectsDuplicateIDs(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(srv.URL) + `
resource "wellbeing_employees" "this" {
  employee {
    id    = "mj"
    name  = "Mogens Jensen"
    email = "mj@example.dk"
  }

  employee {
    id    = "mj"
    name  = "Someone Else"
    email = "else@example.dk"
  }
}
`,
				ExpectError: regexp.MustCompile(`(?s)Duplicate employee id`),
			},
		},
	})
}

func TestAccEmployeesRejectsInvalidGender(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(srv.URL) + `
resource "wellbeing_employees" "this" {
  employee {
    id     = "mj"
    name   = "Mogens Jensen"
    email  = "mj@example.dk"
    gender = "unspecified"
  }
}
`,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
}

const twoEmployeeRoster = `
resource "wellbeing_employees" "this" {
%s
  employee {
    id    = "mj"
    name  = "Mogens Jensen"
    email = "mj@example.dk"
  }

  employee {
    id    = "mogens"
    name  = "Mogens Glistrup"
    email = "mogens@example.dk"
  }
}
`

func TestAccEmployeesBatchLimitBlocksLargeChange(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	// Replacing 100 employees with 2 is a 98% change, far above the default 25%.
	seedRoster(fake, 100)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      fakeProviderConfig(srv.URL) + fmt.Sprintf(twoEmployeeRoster, ""),
				ExpectError: regexp.MustCompile(`exceeds the configured batch limit`),
			},
		},
	})

	// The guard must run before any write reaches the API.
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.puts != 0 {
		t.Errorf("PUT count = %d, want 0 — the batch limit must block before writing", fake.puts)
	}
	if len(fake.employees) != 100 {
		t.Errorf("roster size = %d, want the seeded 100 left untouched", len(fake.employees))
	}
}

func TestAccEmployeesBatchLimitZeroDisablesGuard(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	seedRoster(fake, 100)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// The same 98% change succeeds once the guard is explicitly disabled.
				Config: fakeProviderConfig(srv.URL) + fmt.Sprintf(twoEmployeeRoster, "  batch_limit_percent = 0\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("wellbeing_employees.this", "batch_limit_percent", "0"),
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.#", "2"),
				),
			},
		},
	})
}

func TestAccEmployeesBatchLimitRejectsNegative(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      fakeProviderConfig(srv.URL) + fmt.Sprintf(twoEmployeeRoster, "  batch_limit_percent = -1\n"),
				ExpectError: regexp.MustCompile(`(?s)batch_limit_percent.*must be at least 0`),
			},
		},
	})
}

// TestAccEmployeesFromYAML exercises the shape this provider is expected to be
// driven from: a company-wide people file decoded in Terraform, where
// employment status and the Wellbeing dimensions are all derived from one flat
// roles list.
func TestAccEmployeesFromYAML(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(srv.URL) + `
locals {
  users = yamldecode(<<-YAML
    users:
      mogens@example.dk:
        name: Mogens Glistrup
        phone: "+4513131313"
        roles: [odense, consultant, intern, employee, vpn, on_leave]
      ci@example.dk:
        name: CI Robot
        roles: [employee, vpn]
      mj@example.dk:
        name: Mogens Jensen
        phone: "+4512121212"
        roles: [copenhagen, partner, employee, vpn]
  YAML
  ).users

  # Order is precedence: the first candidate an employee holds wins.
  dimension_candidates = {
    Location = ["copenhagen", "odense", "aarhus"]
    Role     = ["partner", "manager", "consultant", "apprentice", "intern"]
  }

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
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.#", "3"),

					// for_each over a map iterates in sorted key order.
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.0.id", "mogens@example.dk"),
					// on_leave in the roles list drives active.
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.0.active", "false"),
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.0.dimensions.Location", "odense"),
					// Mogens holds both consultant and intern; candidate order wins.
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.0.dimensions.Role", "consultant"),

					// No location or job role at all: the keys are dropped rather
					// than set blank, and the empty map round trips as an empty map.
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.1.id", "ci@example.dk"),
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.1.dimensions.%", "0"),

					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.2.id", "mj@example.dk"),
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.2.active", "true"),
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.2.dimensions.Location", "copenhagen"),
					resource.TestCheckResourceAttr("wellbeing_employees.this", "employee.2.dimensions.Role", "partner"),
				),
			},
		},
	})
}
