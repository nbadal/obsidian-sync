package auth

import (
	"encoding/json"
	"fmt"
	"github.com/nbadal/obsidian-sync/api"
	"io"
)

func Login(email, password string) (error, string) {
	// Create request body
	reqBody := []byte(fmt.Sprintf(`{"email":"%s","password":"%s"}`, email, password))

	// send request
	resp, err := api.SendPostRequest("/user/signin", reqBody)
	if err != nil {
		return err, ""
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("could not read response body: %v", err), ""
	}

	// Parse response body
	var data map[string]interface{}
	err = json.Unmarshal(body, &data)
	if err != nil {
		return fmt.Errorf("could not parse response body: %v", err), ""
	}

	// Check for error
	if data["error"] != nil {
		return fmt.Errorf("error logging in: %s", data["error"]), ""
	}

	// Get token
	token := data["token"].(string)
	if token == "" {
		return fmt.Errorf("token not found in response"), ""
	}

	return nil, token
}
func StoreToken(token string) error {
	fmt.Printf("TODO: Store token %s\n", token)
	return nil
}
