package wellbeingclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

func (c *Client) apiCallsPath() string {
	return fmt.Sprintf("/v1.0/Company/%s/ApiCalls", url.PathEscape(c.companyID))
}

// ListAPICalls returns the company's recent API operations and their status.
func (c *Client) ListAPICalls(ctx context.Context) ([]APICall, error) {
	var out []APICall
	if _, err := c.do(ctx, http.MethodGet, c.apiCallsPath(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAPICall returns the operation with the given id, or (nil, nil) when the
// API has not listed it yet. A queued operation can take a moment to appear.
func (c *Client) GetAPICall(ctx context.Context, operationID string) (*APICall, error) {
	calls, err := c.ListAPICalls(ctx)
	if err != nil {
		return nil, err
	}

	for i := range calls {
		if calls[i].ApiOperationID == operationID {
			return &calls[i], nil
		}
	}
	return nil, nil
}

// WaitForOperation blocks until the queued operation reaches a terminal state.
//
// The documented lifecycle is: HttpStatusCode empty on creation, 202 with
// reason "Accepted" once validated and queued, 202 with "Processing..." while
// running, then 200 on success or a 4xx/5xx on failure. Anything below 300 that
// is not 202 is treated as success.
//
// Bound the wait by giving ctx a deadline.
func (c *Client) WaitForOperation(ctx context.Context, operationID string) error {
	interval := c.pollInitial

	for {
		call, err := c.GetAPICall(ctx, operationID)
		if err != nil {
			return err
		}

		if call != nil && call.HttpStatusCode.Set {
			switch code := call.HttpStatusCode.Int(); {
			case code >= 400:
				return &APIError{
					StatusCode:     code,
					Message:        call.HttpStatusReason,
					ApiOperationID: operationID,
				}
			case code != http.StatusAccepted && code < 300:
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wellbeing: waiting for operation %s: %w", operationID, ctx.Err())
		case <-time.After(interval):
		}

		if interval < c.pollMaximum {
			interval *= 2
			if interval > c.pollMaximum {
				interval = c.pollMaximum
			}
		}
	}
}
