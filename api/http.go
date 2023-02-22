package api

import (
	"bytes"
	"fmt"
	"net/http"
)

func SendPostRequest(endpoint string, body []byte) (*http.Response, error) {
	// Create request
	req, err := http.NewRequest("POST", "https://api.obsidian.md"+endpoint, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("ould not create request: %v", err)
	}

	// Set required headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "app://obsidian.md")

	// send request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %v", err)
	}

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed: (%d) %s", resp.StatusCode, resp.Status)
	}

	return resp, nil
}
