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

func fakeProviderConfig(host string) string {
	return fmt.Sprintf(`
provider "wellbeing" {
  host       = %q
  token      = "test-token"
  company_id = "1000"
}
`, host)
}

func TestAccEmployeeRosterCreateAndUpdate(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(srv.URL) + `
resource "wellbeing_employee_roster" "this" {
  employee = {
    "emp-1" = {
      firstname         = "Bilbo"
      lastname          = "Baggins"
      email             = "bilbo@shire.test"
      employment_status = "active"
      gender            = "unknown"
      dimensions = {
        Department = "Adventuring"
        Location   = "The Shire"
      }
    }
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "employee.%", "1"),
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "employee.emp-1.firstname", "Bilbo"),
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "employee.emp-1.employment_status", "active"),
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "employee.emp-1.fullname", "Bilbo Baggins"),
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "employee.emp-1.sourced_from_external_system", "true"),
					// Department was hoisted on write and folded back on read.
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "employee.emp-1.dimensions.Department", "Adventuring"),
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "employee.emp-1.dimensions.Location", "The Shire"),
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "id", "1000"),
				),
			},
			{
				// Adding a second employee must not disturb the first.
				Config: fakeProviderConfig(srv.URL) + `
resource "wellbeing_employee_roster" "this" {
  employee = {
    "emp-1" = {
      firstname         = "Bilbo"
      lastname          = "Baggins"
      email             = "bilbo@shire.test"
      employment_status = "on_leave"
      gender            = "unknown"
      dimensions = {
        Department = "Adventuring"
        Location   = "The Shire"
      }
    }
    "emp-2" = {
      firstname         = "Samwise"
      lastname          = "Gamgee"
      email             = "sam@shire.test"
      employment_status = "active"
    }
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "employee.%", "2"),
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "employee.emp-1.employment_status", "on_leave"),
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "employee.emp-2.firstname", "Samwise"),
				),
			},
		},
	})
}

func TestAccEmployeeRosterRejectsInvalidEnum(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(srv.URL) + `
resource "wellbeing_employee_roster" "this" {
  employee = {
    "emp-1" = {
      firstname         = "Bilbo"
      lastname          = "Baggins"
      email             = "bilbo@shire.test"
      employment_status = "retired"
    }
  }
}
`,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
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
			Email:            fmt.Sprintf("seed%d@shire.test", i),
			EmploymentStatus: 0,
		})
	}
}

const twoEmployeeRoster = `
resource "wellbeing_employee_roster" "this" {
%s
  employee = {
    "emp-1" = {
      firstname         = "Bilbo"
      lastname          = "Baggins"
      email             = "bilbo@shire.test"
      employment_status = "active"
    }
    "emp-2" = {
      firstname         = "Samwise"
      lastname          = "Gamgee"
      email             = "sam@shire.test"
      employment_status = "active"
    }
  }
}
`

func TestAccEmployeeRosterBatchLimitDefaultsTo25(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// batch_limit_percent is not set; the API documentation recommends 25.
				Config: fakeProviderConfig(srv.URL) + fmt.Sprintf(twoEmployeeRoster, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "batch_limit_percent", "25"),
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "employee.%", "2"),
				),
			},
		},
	})
}

func TestAccEmployeeRosterBatchLimitBlocksLargeChange(t *testing.T) {
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

func TestAccEmployeeRosterBatchLimitZeroDisablesGuard(t *testing.T) {
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
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "batch_limit_percent", "0"),
					resource.TestCheckResourceAttr("wellbeing_employee_roster.this", "employee.%", "2"),
				),
			},
		},
	})
}

func TestAccEmployeeRosterBatchLimitRejectsNegative(t *testing.T) {
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
