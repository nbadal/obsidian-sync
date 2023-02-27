package api

import (
	"encoding/json"
	"fmt"
)

// nextMessageMatching returns the next message from the websocket that matches the given matcher function
// It checks the filtered queue first, and if no message matches, it reads from the websocket
// If no message matches, the function blocks until a matching message is received
// If a message is received that does not match, it is added to a queue
func (ctx *ObsidianSocketContext) nextMessageMatching(matcher func([]byte) bool) ([]byte, error) {
	// Check if there is a message in the filtered queue
	for i, msg := range ctx.filteredQueue {
		if matcher(msg) {
			// Remove message from filtered queue
			if i == 0 {
				ctx.filteredQueue = ctx.filteredQueue[1:]
			} else if i == len(ctx.filteredQueue)-1 {
				ctx.filteredQueue = ctx.filteredQueue[:i]
			} else {
				ctx.filteredQueue = append(ctx.filteredQueue[:i], ctx.filteredQueue[i+1:]...)
			}
			fmt.Printf("⏸️ %s\n", jsonOrBinary(msg))

			return msg, nil
		}
	}
	// Otherwise, read from websocket
	for {
		msg, err := ctx.nextMessage()
		if err != nil {
			return nil, fmt.Errorf("could not read message: %v", err)
		}
		// Return matching message, or add to filtered queue
		if matcher(msg) {
			fmt.Printf("✅ %s\n", jsonOrBinary(msg))
			return msg, nil
		} else {
			// TODO: Throw error if queue is too long
			ctx.filteredQueue = append(ctx.filteredQueue, msg)
		}
	}
}

// nextMessageMatchingJson returns the next message from the websocket that matches the given JSON matcher function
func (ctx *ObsidianSocketContext) nextMessageMatchingJson(matcher func(map[string]interface{}) bool) ([]byte, error) {
	return ctx.nextMessageMatching(func(msg []byte) bool {
		var msgMap map[string]interface{}
		if err := json.Unmarshal(msg, &msgMap); err != nil {
			return false
		}
		return matcher(msgMap)
	})
}

// nextMessageWithJsonKeys returns the next message from the websocket that contains the given JSON keys
func (ctx *ObsidianSocketContext) nextMessageWithJsonKeys(keys ...string) ([]byte, error) {
	return ctx.nextMessageMatchingJson(func(msgMap map[string]interface{}) bool {
		for _, key := range keys {
			if _, ok := msgMap[key]; !ok {
				return false
			}
		}
		return true
	})
}

// nextMessageWithJsonValue returns the next message from the websocket that contains the given JSON key and value
func (ctx *ObsidianSocketContext) nextMessageWithJsonValue(key string, value interface{}) ([]byte, error) {
	return ctx.nextMessageMatchingJson(func(msgMap map[string]interface{}) bool {
		if msgMap[key] != value {
			return false
		}
		return true
	})
}

// nextMessageWithJsonValues returns the next binary (non-JSON) message from the websocket
func (ctx *ObsidianSocketContext) nextBinaryMessage() ([]byte, error) {
	return ctx.nextMessageMatching(func(msg []byte) bool {
		// Message is binary if we can't unmarshal JSON
		var msgMap map[string]interface{}
		err := json.Unmarshal(msg, &msgMap)
		return err != nil
	})
}
