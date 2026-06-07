package fortnox

import (
	"context"
	"fmt"
)

// UploadToInbox uploads a file to the Fortnox inbox and returns the file ID.
func (c *Client) UploadToInbox(ctx context.Context, filename string, data []byte) (string, error) {
	var resp InboxUploadResponse

	if err := c.uploadMultipart(ctx, "/inbox", filename, data, &resp); err != nil {
		return "", fmt.Errorf("uploading to Fortnox inbox: %w", err)
	}

	if resp.File.ID == "" {
		return "", fmt.Errorf("Fortnox inbox upload did not return a file ID")
	}

	return resp.File.ID, nil
}
