package wellbeingclient

import (
	"encoding/json"
	"testing"
)

func TestFlexBoolUnmarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    bool
		wantErr bool
	}{
		{name: "bool true", input: `true`, want: true},
		{name: "bool false", input: `false`, want: false},
		{name: "string true", input: `"true"`, want: true},
		{name: "string false", input: `"false"`, want: false},
		{name: "string True mixed case", input: `"True"`, want: true},
		{name: "null", input: `null`, want: false},
		{name: "garbage", input: `"banana"`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got FlexBool
			err := json.Unmarshal([]byte(tt.input), &got)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Unmarshal(%s) = nil error, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal(%s) returned error: %v", tt.input, err)
			}
			if got.Bool() != tt.want {
				t.Errorf("Unmarshal(%s) = %v, want %v", tt.input, got.Bool(), tt.want)
			}
		})
	}
}

func TestFlexIntUnmarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantSet bool
		want    int
		wantErr bool
	}{
		{name: "number", input: `202`, wantSet: true, want: 202},
		{name: "zero", input: `0`, wantSet: true, want: 0},
		{name: "numeric string", input: `"404"`, wantSet: true, want: 404},
		{name: "empty string", input: `""`, wantSet: false},
		{name: "null", input: `null`, wantSet: false},
		{name: "garbage", input: `"banana"`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got FlexInt
			err := json.Unmarshal([]byte(tt.input), &got)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Unmarshal(%s) = nil error, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal(%s) returned error: %v", tt.input, err)
			}
			if got.Set != tt.wantSet {
				t.Errorf("Unmarshal(%s).Set = %v, want %v", tt.input, got.Set, tt.wantSet)
			}
			if got.Set && got.Int() != tt.want {
				t.Errorf("Unmarshal(%s) = %d, want %d", tt.input, got.Int(), tt.want)
			}
		})
	}
}
