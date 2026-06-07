package fortnox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type StoredToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func LoadToken(path string) (*StoredToken, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var t StoredToken
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}

	return &t, nil
}

func SaveToken(path string, t *StoredToken) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("creating token directory: %w", err)
	}

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}
