package wellbeingclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// ListEnabledLanguages returns the languages enabled for the company.
func (c *Client) ListEnabledLanguages(ctx context.Context) ([]Language, error) {
	path := fmt.Sprintf("/v1.0/Company/%s/Language/Enabled", url.PathEscape(c.companyID))

	var out []Language
	if _, err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
