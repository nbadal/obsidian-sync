package api

import (
	"encoding/json"
	"fmt"
)

func jsonOrBinary(data []byte) string {
	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Sprintf("Binary [%d]", len(data))
	} else {
		return string(data)
	}
}
