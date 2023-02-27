package api

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
)

type SocketMessageSender interface {
	sendMessage(msg interface{}) error
	sendBinary(msg []byte) error
}

type SocketMessageReceiver interface {
	nextMessage() ([]byte, error)
}

type SocketConnection interface {
	connect(url string) error
	Close() error
}

func (ctx *ObsidianSocketContext) connect(url string) error {
	conn, _, err := websocket.DefaultDialer.Dial("wss://"+url+"/", nil)
	if err != nil {
		return err
	}
	ctx.ws = conn
	return nil
}

// send sends a message to the websocket
func (ctx *ObsidianSocketContext) sendMessage(msg interface{}) error {
	if err := ctx.ws.WriteJSON(msg); err != nil {
		return fmt.Errorf("could not send message: %v", err)
	}

	// Log JSON message
	jsonMsg, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("could not marshal message: %v", err)
	}
	fmt.Printf("⏩ %s\n", jsonMsg)

	return nil
}

func (ctx *ObsidianSocketContext) sendBinary(msg []byte) error {
	if err := ctx.ws.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		return fmt.Errorf("could not send message: %v", err)
	}

	// Log binary message
	fmt.Printf("⏩ Binary [%d]\n", len(msg))

	return nil
}

// nextMessage returns the next message from the websocket
func (ctx *ObsidianSocketContext) nextMessage() ([]byte, error) {
	_, msg, err := ctx.ws.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("error reading message: %v", err)
	}
	fmt.Printf("⏪ %s\n", jsonOrBinary(msg))
	return msg, nil
}

func (ctx *ObsidianSocketContext) Close() error {
	return ctx.ws.Close()
}
