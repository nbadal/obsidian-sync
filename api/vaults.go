package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
)

func ListVaults(token string) ([]VaultInfo, error) {
	body, err := json.Marshal(map[string]string{
		"token": token,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create vault list request: %v", err)
	}

	// send request
	resp, err := SendPostRequest("/vault/list", body)
	if err != nil {
		return nil, fmt.Errorf("could not send vault list request: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("could not close vault list response body: %v", err)
		}
	}(resp.Body)

	var data map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("could not decode vault list response: %v", err)
	}
	if errMsg, ok := data["error"]; ok {
		return nil, fmt.Errorf("server returned error: %s", errMsg)
	}
	if data["vaults"] == nil {
		return nil, fmt.Errorf("no vaults returned")
	}

	// Decode vaults and return
	var vaults []VaultInfo
	if err := json.Unmarshal(data["vaults"], &vaults); err != nil {
		return nil, fmt.Errorf("could not decode vault list response: %v", err)
	}
	return vaults, nil
}

type VaultInfo struct {
	Id       string `json:"id"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Salt     string `json:"salt"`
	Host     string `json:"host"`
}
