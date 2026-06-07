package fortnox

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func decodeResponse(resp *http.Response, out any) error {
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return fmt.Errorf("fortnox API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
		return fmt.Errorf("decoding Fortnox response: %w", err)
	}

	return nil
}
