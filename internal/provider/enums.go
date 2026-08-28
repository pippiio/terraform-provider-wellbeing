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

// Survey enums. Unlike gender and employment status, the API performs no
// server-side validation of these: it accepted every Frequency integer tried
// during the API spike, including 99, storing the value verbatim. The guard
// therefore exists only here, which makes the map the single line of defence
// rather than a convenience.
var (
	// questionTypeCodes covers the question types a user writes. The welcome and
	// thankyou steps use codes 1 and 99 but are synthesised by the provider from
	// first_page and last_page, so they are deliberately absent.
	questionTypeCodes = map[string]int{
		"option": 2,
		"prompt": 3,
	}

	surveyStateCodes = map[string]int{
		"draft":    1,
		"active":   2,
		"inactive": 3,
	}

	// surveyFrequencyCodes holds only the values whose meaning is confirmed.
	// Others exist but are unattributed, and guessing would mislabel a survey's
	// cadence silently.
	surveyFrequencyCodes = map[string]int{
		"hourly":    1,
		"quarterly": 3,
	}
)

func questionTypeToAPI(name string) (int, bool) {
	code, ok := questionTypeCodes[name]
	return code, ok
}

func questionTypeFromAPI(code int) (string, bool) { return lookupName(questionTypeCodes, code) }

func questionTypeValues() []string { return sortedKeys(questionTypeCodes) }

func surveyStateToAPI(name string) (int, bool) {
	code, ok := surveyStateCodes[name]
	return code, ok
}

func surveyStateFromAPI(code int) (string, bool) { return lookupName(surveyStateCodes, code) }

func surveyStateValues() []string { return sortedKeys(surveyStateCodes) }

func surveyFrequencyToAPI(name string) (int, bool) {
	code, ok := surveyFrequencyCodes[name]
	return code, ok
}

func surveyFrequencyFromAPI(code int) (string, bool) { return lookupName(surveyFrequencyCodes, code) }

func surveyFrequencyValues() []string { return sortedKeys(surveyFrequencyCodes) }
