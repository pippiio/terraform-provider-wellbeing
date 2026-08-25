package provider

import (
	"maps"
	"slices"
)

// The Wellbeing API encodes gender and employment status as integers. The
// provider exposes validated strings instead, so configs are self-documenting
// and typos fail at plan time rather than silently mislabelling a person.
var (
	genderCodes = map[string]int{
		"male":    0,
		"female":  1,
		"unknown": 9,
	}

	employmentStatusCodes = map[string]int{
		"active":   0,
		"on_leave": 1,
	}
)

func genderToAPI(name string) (int, bool) {
	code, ok := genderCodes[name]
	return code, ok
}

func genderFromAPI(code int) (string, bool) {
	return lookupName(genderCodes, code)
}

func genderValues() []string {
	return sortedKeys(genderCodes)
}

func employmentStatusToAPI(name string) (int, bool) {
	code, ok := employmentStatusCodes[name]
	return code, ok
}

func employmentStatusFromAPI(code int) (string, bool) {
	return lookupName(employmentStatusCodes, code)
}

func employmentStatusValues() []string {
	return sortedKeys(employmentStatusCodes)
}

func lookupName(codes map[string]int, code int) (string, bool) {
	for name, candidate := range codes {
		if candidate == code {
			return name, true
		}
	}
	return "", false
}

// sortedKeys keeps generated documentation and validator error messages stable
// across runs; Go map iteration order is deliberately randomised.
func sortedKeys(codes map[string]int) []string {
	return slices.Sorted(maps.Keys(codes))
}
