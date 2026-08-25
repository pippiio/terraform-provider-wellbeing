package wellbeingclient

import (
	"context"
	"net/http"
	"testing"
)

func TestListEnabledLanguages(t *testing.T) {
	t.Parallel()

	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/Company/1000/Language/Enabled" {
			t.Errorf("path = %q, want /v1.0/Company/1000/Language/Enabled", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":1045,"label":"English","code":"en"},{"id":1041,"label":"Dansk","code":"da"}]`))
	}))

	got, err := c.ListEnabledLanguages(context.Background())
	if err != nil {
		t.Fatalf("ListEnabledLanguages returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != 1045 || got[0].Code != "en" {
		t.Errorf("got[0] = %+v, want id 1045 code en", got[0])
	}
}
