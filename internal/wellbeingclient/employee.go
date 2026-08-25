package wellbeingclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

func (c *Client) employeePath() string {
	return fmt.Sprintf("/v1.0/Company/%s/Employee", url.PathEscape(c.companyID))
}

// ListEmployees returns the company's employees.
//
// The API returns only employees that were created through the API; employees
// created in the Wellbeing portal are not included.
func (c *Client) ListEmployees(ctx context.Context) ([]Employee, error) {
	var out []Employee
	if _, err := c.do(ctx, http.MethodGet, c.employeePath(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ReplaceEmployees performs a set-based replace of the entire roster. Employees
// absent from the payload are removed by the API.
//
// A 202 response means the import was queued rather than applied; poll
// WaitForOperation with the returned ApiOperationID to find out how it ended.
func (c *Client) ReplaceEmployees(ctx context.Context, employees []Employee) (*ImportResult, error) {
	// Marshal a nil slice as [] rather than null: the API expects an array, and
	// an explicit null is rejected as malformed.
	if employees == nil {
		employees = []Employee{}
	}

	var out ImportResult
	status, err := c.do(ctx, http.MethodPut, c.employeePath(), employees, &out)
	if err != nil {
		return nil, err
	}

	// The 200 and 202 bodies disagree about WasQueued's encoding and, in
	// practice, its value. The status code is the reliable signal.
	if status == http.StatusAccepted {
		out.WasQueued = true
	}

	return &out, nil
}
