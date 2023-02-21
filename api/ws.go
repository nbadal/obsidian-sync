package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/nbadal/obsidian-sync/crypto"
	"log"
	"math/rand"
	"time"
)

type IncomingPushMessage struct {
	Op      string `json:"op"`
	Path    string `json:"path"`
	Hash    string `json:"hash"`
	Size    int64  `json:"size"`
	Ctime   int64  `json:"ctime"`
	Mtime   int64  `json:"mtime"`
	Folder  bool   `json:"folder"`
	Deleted bool   `json:"deleted"`
	Device  string `json:"device"`
	Uid     int64  `json:"uid"`
}

type OutgoingPushMessage struct {
	Op      string `json:"op"`
	Path    string `json:"path"`
	Ext     string `json:"extension"`
	Hash    string `json:"hash"`
	Ctime   int64  `json:"ctime"`
	Mtime   int64  `json:"mtime"`
	Folder  bool   `json:"folder"`
	Deleted bool   `json:"deleted"`
	Size    int64  `json:"size"`
	Pieces  int    `json:"pieces"`
}

type PullHeaderMessage struct {
	Op     string `json:"op"`
	Hash   string `json:"hash"`
	Size   int64  `json:"size"`
	Pieces int    `json:"pieces"`
}

type ReadyMessage struct {
	Op      string `json:"op"`
	Version int    `json:"version"`
}

// PullSession represents a session for a specific push operation
type PullSession struct {
	UID  int64
	Path string
	Hash string
}

type ObsidianFile struct {
	Path      string
	LatestUID int64
}

type ObsidianSocketContext struct {
	ws                *websocket.Conn
	Vault             VaultInfo
	handlers          *MessageHandlers
	authToken         string
	keyhash           string
	MessageLog        chan string
	unhandledMessages chan []byte
}

type MessageHandler func(ctx *ObsidianSocketContext, msg []byte) error

type MessageHandlers struct {
	opMessageHandlers    map[string]MessageHandler
	noOpMessageHandler   MessageHandler
	binaryMessageHandler MessageHandler
}

func ConnectToVault(vault VaultInfo, password string, authToken string, handlers *MessageHandlers) (*ObsidianSocketContext, error) {
	// Generate keyhash for vault
	keyhash, err := crypto.KeyHash([]byte(password), []byte(vault.Salt))
	if err != nil {
		return nil, fmt.Errorf("error generating keyhash: %s", err)
	}

	// Connect to websocket
	conn, _, err := websocket.DefaultDialer.Dial("wss://"+vault.Host+"/", nil)
	if err != nil {
		return nil, fmt.Errorf("error connecting to websocket: %s", err)
	}

	ctx := &ObsidianSocketContext{
		ws:                conn,
		Vault:             vault,
		authToken:         authToken,
		keyhash:           keyhash,
		handlers:          handlers,
		MessageLog:        make(chan string, 100),
		unhandledMessages: make(chan []byte, 100),
	}

	// Start handling messages
	go func() {
		err := ctx.handleMessages()
		if err != nil {
			log.Fatalf("error handling messages: %v", err)
		}
	}()

	// Start listening for messages
	go func() {
		err := ctx.listenForMessages()
		if err != nil {
			log.Fatalf("error listening for messages: %v", err)
		}
	}()

	// Start sending pings to keep the connection alive
	go func() {
		err := ctx.keepAlive()
		if err != nil {
			log.Fatalf("error sending pings: %v", err)
		}
	}()

	return ctx, nil
}

// listenForMessages listens for messages from the websocket and sends them to the message queue
func (ctx *ObsidianSocketContext) listenForMessages() error {
	for {
		_, message, err := ctx.ws.ReadMessage()
		if err != nil {
			return fmt.Errorf("could not read message: %v", err)
		}
		ctx.unhandledMessages <- message
	}
}

// getNextUnhandledMessage gets the next message from the message queue and logs it
func (ctx *ObsidianSocketContext) getNextUnhandledMessage() []byte {
	message := <-ctx.unhandledMessages
	data := make(map[string]interface{})
	if err := json.Unmarshal(message, &data); err != nil {
		ctx.MessageLog <- fmt.Sprintf("⏪ Binary [%d]", len(message))
	} else {
		ctx.MessageLog <- "⏪ " + string(message)
	}
	return message
}

// handleMessages handles messages from the message queue
func (ctx *ObsidianSocketContext) handleMessages() error {
	var num = 1
	for {
		ctx.MessageLog <- fmt.Sprintf("Handling message %d", num)
		message := ctx.getNextUnhandledMessage()
		if err := ctx.handleMessage(message); err != nil {
			return fmt.Errorf("error handling message: %v", err)
		}
		ctx.MessageLog <- fmt.Sprintf("Handled message %d", num)
		num++
	}
}

// handleMessage routes a message to the correct handler, if one exists
func (ctx *ObsidianSocketContext) handleMessage(bytes []byte) error {
	data := make(map[string]interface{})
	if err := json.Unmarshal(bytes, &data); err == nil {
		if op, ok := data["op"]; ok {
			// Ignore pong messages
			if op.(string) == "pong" {
				return nil
			}

			// Handle op messages
			if handler, ok := ctx.handlers.opMessageHandlers[op.(string)]; ok {
				if err := handler(ctx, bytes); err != nil {
					return fmt.Errorf("error handling op (%s) message: %v", op.(string), err)
				}
			} else {
				return fmt.Errorf("no handler for op: %s", op)
			}
		} else {
			// Handle messages without an op
			if ctx.handlers.noOpMessageHandler != nil {
				if err := ctx.handlers.noOpMessageHandler(ctx, bytes); err != nil {
					return fmt.Errorf("error handling no-op message: %v", err)
				}
			} else {
				return fmt.Errorf("no handler for message without op")
			}
		}
	} else {
		// Handle binary messages
		if ctx.handlers.binaryMessageHandler != nil {
			if err := ctx.handlers.binaryMessageHandler(ctx, bytes); err != nil {
				return fmt.Errorf("error handling binary message: %v", err)
			}
		} else {
			return fmt.Errorf("no handler for binary message")
		}
	}
	return nil
}

// RegisterOpHandler registers a callback for a specific op type
func (h *MessageHandlers) RegisterOpHandler(op string, callback MessageHandler) {
	if h.opMessageHandlers == nil {
		h.opMessageHandlers = make(map[string]MessageHandler)
	}
	h.opMessageHandlers[op] = callback
}

// RegisterNoOpHandler registers a callback for a message without an op
func (h *MessageHandlers) RegisterNoOpHandler(callback MessageHandler) {
	h.noOpMessageHandler = callback
}

// RegisterBinaryHandler registers a callback for a message that cant be unmarshalled
func (h *MessageHandlers) RegisterBinaryHandler(callback MessageHandler) {
	h.binaryMessageHandler = callback
}

// Send sends a message to the websocket
func (ctx *ObsidianSocketContext) Send(msg interface{}) error {
	if err := ctx.ws.WriteJSON(msg); err != nil {
		return fmt.Errorf("could not send message: %v", err)
	}

	// Log JSON message
	jsonMsg, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("could not marshal message: %v", err)
	}
	ctx.MessageLog <- "⏩ " + string(jsonMsg)

	return nil
}

func (ctx *ObsidianSocketContext) SendBinary(msg []byte) error {
	if err := ctx.ws.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		return fmt.Errorf("could not send message: %v", err)
	}

	// Log binary message
	ctx.MessageLog <- fmt.Sprintf("⏩ Binary [%d]", len(msg))

	return nil
}

// keepAlive sends a ping message every 20-30 seconds to keep the connection alive
func (ctx *ObsidianSocketContext) keepAlive() error {
	for {
		time.Sleep(time.Duration(rand.Intn(10)+20) * time.Second)
		if err := ctx.Send(struct {
			Op string `json:"op"`
		}{
			Op: "ping",
		}); err != nil {
			return err
		}
	}
}

// SendInit sends the initial JSON message to the websocket
func (ctx *ObsidianSocketContext) SendInit() error {
	initialMsg := struct {
		Op      string `json:"op"`
		ID      string `json:"id"`
		Token   string `json:"token"`
		Keyhash string `json:"keyhash"`
		Version int    `json:"version"`
		Initial bool   `json:"initial"`
		Device  string `json:"device"`
	}{
		Op:      "init",
		ID:      ctx.Vault.Id,
		Token:   ctx.authToken,
		Keyhash: ctx.keyhash,
		Version: 0,               // TODO: This should be the latest UID we're aware of
		Initial: true,            // TODO: False if this isn't our first sync
		Device:  "obsidian-sync", // TODO: Allow a device name override
	}
	if err := ctx.Send(initialMsg); err != nil {
		return fmt.Errorf("could not send initial message: %v", err)
	}

	return nil
}

func (ctx *ObsidianSocketContext) Close() {
	err := ctx.ws.Close()
	if err != nil {
		panic(err)
	}
}

func (ctx *ObsidianSocketContext) SendAndHandleSizeOp() error {
	// Send size op
	if err := ctx.Send(struct {
		Op string `json:"op"`
	}{
		Op: "size",
	}); err != nil {
		return fmt.Errorf("could not send size op: %v", err)
	}

	// Next message should be a size response
	response := ctx.getNextUnhandledMessage()

	type SizeResponse struct {
		Size  int64 `json:"size"`
		Limit int64 `json:"limit"`
	}
	var sizeResponse SizeResponse
	if err := json.Unmarshal(response, &sizeResponse); err != nil {
		return fmt.Errorf("could not unmarshal size response: %v", err)
	}

	ctx.MessageLog <- fmt.Sprintf("Size: %d/%d", sizeResponse.Size, sizeResponse.Limit)

	// TODO: What does this size response represent?

	return nil
}

// PullFile initiates a pull for a file, which should send a header and binary data
// TODO: This should also support a deletion result
func (ctx *ObsidianSocketContext) PullFile(uid int64, expectedHash string) ([]byte, error) {
	// Send a pull op for this UID
	pullMsg := struct {
		Op  string `json:"op"`
		UID int64  `json:"uid"`
	}{
		Op:  "pull",
		UID: uid,
	}

	if err := ctx.Send(pullMsg); err != nil {
		return nil, fmt.Errorf("could not send pull message: %v", err)
	}

	// Next message should be a header
	header := ctx.getNextUnhandledMessage()

	// Unmarshal pull header message
	var headerMessage PullHeaderMessage
	err := json.Unmarshal(header, &headerMessage)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal pull header message: %v", err)
	}

	ctx.MessageLog <- fmt.Sprintf("ℹ️ Received header for %d", uid)
	ctx.MessageLog <- fmt.Sprintf("ℹ️ Size: %d", headerMessage.Size)
	ctx.MessageLog <- fmt.Sprintf("ℹ️ Pieces: %d", headerMessage.Pieces)

	var data []byte
	// Intercept N websocket messages and append them to the session data
	for i := 0; i < headerMessage.Pieces; i++ {
		message := ctx.getNextUnhandledMessage()
		data = append(data, message...)
	}

	// Ensure that our byte count matches the size
	if int64(len(data)) != headerMessage.Size {
		return nil, fmt.Errorf("decrypted data size does not match size in header")
	}

	// Decrypt the decryptedData
	decryptedData, err := crypto.Decrypt(data, []byte(ctx.Vault.Password), []byte(ctx.Vault.Salt))
	if err != nil {
		return nil, fmt.Errorf("could not decrypt data: %v", err)
	}

	//Ensure the SHA-256 encryptedHash matches
	contentSum := sha256.Sum256(decryptedData)

	expectedHashBytes, err := hex.DecodeString(expectedHash)
	if err != nil {
		return nil, fmt.Errorf("could not decode expected hash: %v", err)
	}

	if !bytes.Equal(contentSum[:], expectedHashBytes) {
		return nil, fmt.Errorf("decrypted content hash does not match expected hash")
	}

	return decryptedData, nil
}

func (ctx *ObsidianSocketContext) PushFile(path string, extension string, ctime int64, mtime int64, folder bool, deleted bool, content []byte) error {
	// Encrypt the content
	encryptedContent, err := crypto.Encrypt(content, []byte(ctx.Vault.Password), []byte(ctx.Vault.Salt))
	if err != nil {
		return fmt.Errorf("could not encrypt content: %v", err)
	}

	// Encrypt the path
	encryptedPath, err := crypto.Encrypt([]byte(path), []byte(ctx.Vault.Password), []byte(ctx.Vault.Salt))
	if err != nil {
		return fmt.Errorf("could not encrypt path: %v", err)
	}

	// Calculate the SHA-256 encryptedHash of the encrypted content
	contentSum := sha256.Sum256(content)

	// Encrypt the content sum
	encryptedContentSum, err := crypto.Encrypt(contentSum[:], []byte(ctx.Vault.Password), []byte(ctx.Vault.Salt))
	if err != nil {
		return fmt.Errorf("could not encrypt content sum: %v", err)
	}

	message := &OutgoingPushMessage{
		Op:      "push",
		Path:    hex.EncodeToString(encryptedPath),
		Ext:     extension,
		Hash:    hex.EncodeToString(encryptedContentSum),
		Ctime:   ctime,
		Mtime:   mtime,
		Folder:  folder,
		Deleted: deleted,
		Size:    int64(len(encryptedContent)),
		Pieces:  1,
	}

	if err := ctx.Send(message); err != nil {
		return fmt.Errorf("could not send push message: %v", err)
	}

	// Next message should be a {"res": "next"}
	response := ctx.getNextUnhandledMessage()
	var nextResponse struct {
		Res string `json:"res"`
	}
	if err := json.Unmarshal(response, &nextResponse); err != nil {
		return fmt.Errorf("could not unmarshal next response: %v", err)
	}
	if nextResponse.Res != "next" {
		return fmt.Errorf("next response is not 'next'")
	}

	// Send the encrypted content
	if err := ctx.SendBinary(encryptedContent); err != nil {
		return fmt.Errorf("could not send encrypted content: %v", err)
	}

	// Next message should be an incoming push
	response = ctx.getNextUnhandledMessage()
	var pushResponse IncomingPushMessage
	if err := json.Unmarshal(response, &pushResponse); err != nil {
		return fmt.Errorf("could not unmarshal push response: %v", err)
	}
	// TODO: Make sure the incoming push response matches the outgoing push message

	// Next message should be an {"op": "ok"}
	response = ctx.getNextUnhandledMessage()
	var okResponse struct {
		Op string `json:"op"`
	}
	if err := json.Unmarshal(response, &okResponse); err != nil {
		return fmt.Errorf("could not unmarshal ok response: %v", err)
	}
	if okResponse.Op != "ok" {
		return fmt.Errorf("ok response is not 'ok'")
	}

	return nil
}
