package wellbeingclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// FlexBool decodes a JSON boolean that the API sometimes encodes as a string.
// The employee import returns `"WasQueued": "false"` on 200 but `"WasQueued": true`
// on 202, so neither a plain bool nor a plain string field can decode both.
type FlexBool bool

// Bool returns the decoded value.
func (f FlexBool) Bool() bool { return bool(f) }

// UnmarshalJSON accepts true, false, "true", "false" (any case), and null.
func (f *FlexBool) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*f = false
		return nil
	}

	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		*f = FlexBool(b)
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("wellbeing: cannot decode %s as bool", data)
	}

	parsed, err := strconv.ParseBool(s)
	if err != nil {
		return fmt.Errorf("wellbeing: cannot decode %q as bool", s)
	}
	*f = FlexBool(parsed)
	return nil
}

// FlexInt decodes a JSON integer that the API may omit, null, or encode as a
// string. Set distinguishes "absent" from "present and zero" — GET /ApiCalls
// leaves HttpStatusCode empty until a queued request has been validated, and
// treating that as 0 would be indistinguishable from a real status.
type FlexInt struct {
	Set   bool
	Value int
}

// Int returns the decoded value, or 0 when unset.
func (f FlexInt) Int() int { return f.Value }

// UnmarshalJSON accepts a number, a numeric string, an empty string, and null.
func (f *FlexInt) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*f = FlexInt{}
		return nil
	}

	var i int
	if err := json.Unmarshal(data, &i); err == nil {
		*f = FlexInt{Set: true, Value: i}
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("wellbeing: cannot decode %s as int", data)
	}
	if s == "" {
		*f = FlexInt{}
		return nil
	}

	parsed, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("wellbeing: cannot decode %q as int", s)
	}
	*f = FlexInt{Set: true, Value: parsed}
	return nil
}
