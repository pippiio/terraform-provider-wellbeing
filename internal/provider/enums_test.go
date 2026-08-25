package provider

import (
	"slices"
	"testing"
)

func TestGenderRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code int
	}{
		{name: "male", code: 0},
		{name: "female", code: 1},
		{name: "unknown", code: 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			code, ok := genderToAPI(tt.name)
			if !ok {
				t.Fatalf("genderToAPI(%q) not recognised", tt.name)
			}
			if code != tt.code {
				t.Errorf("genderToAPI(%q) = %d, want %d", tt.name, code, tt.code)
			}

			name, ok := genderFromAPI(tt.code)
			if !ok {
				t.Fatalf("genderFromAPI(%d) not recognised", tt.code)
			}
			if name != tt.name {
				t.Errorf("genderFromAPI(%d) = %q, want %q", tt.code, name, tt.name)
			}
		})
	}
}

func TestGenderRejectsUnknownValues(t *testing.T) {
	t.Parallel()

	if _, ok := genderToAPI("nonbinary"); ok {
		t.Error("genderToAPI accepted an undocumented value")
	}
	if _, ok := genderFromAPI(7); ok {
		t.Error("genderFromAPI accepted an undocumented code")
	}
}

func TestGenderValuesAreSorted(t *testing.T) {
	t.Parallel()

	got := genderValues()
	if len(got) != 3 {
		t.Fatalf("genderValues() = %v, want 3 entries", got)
	}
	if !slices.IsSorted(got) {
		t.Errorf("genderValues() = %v, want sorted output for stable docs and error messages", got)
	}
}

func TestEmploymentStatusRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code int
	}{
		{name: "active", code: 0},
		{name: "on_leave", code: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			code, ok := employmentStatusToAPI(tt.name)
			if !ok {
				t.Fatalf("employmentStatusToAPI(%q) not recognised", tt.name)
			}
			if code != tt.code {
				t.Errorf("employmentStatusToAPI(%q) = %d, want %d", tt.name, code, tt.code)
			}

			name, ok := employmentStatusFromAPI(tt.code)
			if !ok {
				t.Fatalf("employmentStatusFromAPI(%d) not recognised", tt.code)
			}
			if name != tt.name {
				t.Errorf("employmentStatusFromAPI(%d) = %q, want %q", tt.code, name, tt.name)
			}
		})
	}
}

func TestEmploymentStatusValues(t *testing.T) {
	t.Parallel()

	got := employmentStatusValues()
	if len(got) != 2 {
		t.Fatalf("employmentStatusValues() = %v, want 2 entries", got)
	}
	if !slices.IsSorted(got) {
		t.Errorf("employmentStatusValues() = %v, want sorted output", got)
	}
}
