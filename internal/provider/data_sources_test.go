package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestDataSourceSchemasAreValid(t *testing.T) {
	t.Parallel()

	constructors := map[string]func() datasource.DataSource{
		"wellbeing_company":           NewCompanyDataSource,
		"wellbeing_employees":         NewEmployeesDataSource,
		"wellbeing_enabled_languages": NewEnabledLanguagesDataSource,
		"wellbeing_survey_templates":  NewSurveyTemplatesDataSource,
		"wellbeing_survey_answers":    NewSurveyAnswersDataSource,
	}

	for wantTypeName, constructor := range constructors {
		t.Run(wantTypeName, func(t *testing.T) {
			t.Parallel()

			ds := constructor()

			metadataResp := &datasource.MetadataResponse{}
			ds.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "wellbeing"}, metadataResp)
			if metadataResp.TypeName != wantTypeName {
				t.Errorf("TypeName = %q, want %q", metadataResp.TypeName, wantTypeName)
			}

			schemaResp := &datasource.SchemaResponse{}
			ds.Schema(context.Background(), datasource.SchemaRequest{}, schemaResp)
			if schemaResp.Diagnostics.HasError() {
				t.Errorf("schema has errors: %v", schemaResp.Diagnostics)
			}
		})
	}
}

func TestAccEnabledLanguagesDataSource(t *testing.T) {
	skipWithoutTerraform(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/Company/1000/Language/Enabled" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"id":1045,"label":"English","code":"en"},{"id":1041,"label":"Dansk","code":"da"}]`))
	}))
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(srv.URL) + `
data "wellbeing_enabled_languages" "this" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.wellbeing_enabled_languages.this", "languages.#", "2"),
					resource.TestCheckResourceAttr("data.wellbeing_enabled_languages.this", "languages.0.code", "en"),
					resource.TestCheckResourceAttr("data.wellbeing_enabled_languages.this", "languages.0.id", "1045"),
				),
			},
		},
	})
}

func TestAccEmployeesDataSource(t *testing.T) {
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
}

data "wellbeing_employees" "this" {
  depends_on = [wellbeing_employees.this]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.wellbeing_employees.this", "employees.%", "1"),
					resource.TestCheckResourceAttr("data.wellbeing_employees.this", "employees.mj.fullname", "Mogens Jensen"),
				),
			},
		},
	})
}

// fakeCompanyHandler serves the two endpoints the company data source composes:
// the employee roster and the enabled languages list.
func fakeCompanyHandler(fake *fakeRoster) http.Handler {
	roster := fake.handler()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0/Company/1000/Language/Enabled" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":1045,"label":"English","code":"en"},{"id":1041,"label":"Dansk","code":"da"}]`))
			return
		}
		roster.ServeHTTP(w, r)
	})
}

func TestAccCompanyDataSource(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fakeCompanyHandler(fake))
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
    id    = "mogens"
    name  = "Mogens Glistrup"
    email = "mogens@example.dk"
  }
}

data "wellbeing_company" "this" {
  depends_on = [wellbeing_employees.this]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// id comes from provider configuration, not from a request.
					resource.TestCheckResourceAttr("data.wellbeing_company.this", "id", "1000"),
					resource.TestCheckResourceAttr("data.wellbeing_company.this", "employee_count", "2"),
					resource.TestCheckResourceAttr("data.wellbeing_company.this", "enabled_languages.#", "2"),
					resource.TestCheckResourceAttr("data.wellbeing_company.this", "enabled_languages.0.code", "en"),
					resource.TestCheckResourceAttr("data.wellbeing_company.this", "enabled_languages.0.id", "1045"),
					resource.TestCheckResourceAttr("data.wellbeing_company.this", "enabled_languages.1.label", "Dansk"),
				),
			},
		},
	})
}

func TestAccCompanyDataSourceEmptyRoster(t *testing.T) {
	skipWithoutTerraform(t)

	fake := &fakeRoster{}
	srv := httptest.NewServer(fakeCompanyHandler(fake))
	t.Cleanup(srv.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(srv.URL) + `
data "wellbeing_company" "this" {}
`,
				Check: resource.TestCheckResourceAttr("data.wellbeing_company.this", "employee_count", "0"),
			},
		},
	})
}
