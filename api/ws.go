package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/nbadal/obsidian-sync/crypto"
	"math/rand"
	"time"
)

type IncomingPushMessage struct {
	Op            string `json:"op"`
	EncryptedPath string `json:"path"`
	EncryptedHash string `json:"hash"`
	Size          int64  `json:"size"`
	Ctime         int64  `json:"ctime"`
	Mtime         int64  `json:"mtime"`
	Folder        bool   `json:"folder"`
	Deleted       bool   `json:"deleted"`
	Device        string `json:"device"`
	Uid           int64  `json:"uid"`
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
	Hash   string `json:"hash"`
	Size   int64  `json:"size"`
	Pieces int    `json:"pieces"`
}

type ObsidianSocketContext struct {
	ws            *websocket.Conn
	Vault         VaultInfo
	authToken     string
	keyhash       string
	filteredQueue [][]byte
}

func ConnectToVault(vault VaultInfo, password string, authToken string) (*ObsidianSocketContext, error) {
	// Generate keyhash for vault
	keyhash, err := crypto.KeyHash([]byte(password), []byte(vault.Salt))
	if err != nil {
		return nil, fmt.Errorf("error generating keyhash: %s", err)
	}

	ctx := &ObsidianSocketContext{
		Vault:         vault,
		authToken:     authToken,
		keyhash:       keyhash,
		filteredQueue: [][]byte{},
	}

	// Connect to websocket
	err = ctx.connect(vault.Host)
	if err != nil {
		return nil, fmt.Errorf("error connecting to websocket: %s", err)
	}

	return ctx, nil
}

type InitResult struct {
	RemoteUid   int64
	PushedFiles []IncomingPushMessage
}

// SendInit sends the initial JSON message to the websocket
func (ctx *ObsidianSocketContext) SendInit() (*InitResult, error) {
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
	if err := ctx.sendMessage(initialMsg); err != nil {
		return nil, fmt.Errorf("could not send init message: %v", err)
	}

	// Next message should be an {res: ok}
	response, err := ctx.nextMessageWithJsonValue("res", "ok")
	if err != nil {
		return nil, fmt.Errorf("error reading message: %v", err)
	}
	var data struct {
		Res string `json:"res"`
	}
	if err := json.Unmarshal(response, &data); err != nil {
		return nil, fmt.Errorf("error unmarshalling message: %v", err)
	}
	if data.Res != "ok" {
		return nil, fmt.Errorf("expected ok message, got %s", data.Res)
	}

	var pushedFiles []IncomingPushMessage
	var remoteUid int64
	for {
		response, err := ctx.nextMessageMatchingJson(func(json map[string]interface{}) bool {
			return json["op"] == "push" || json["op"] == "ready"
		})
		if err != nil {
			return nil, fmt.Errorf("error reading message: %v", err)
		}

		var data map[string]interface{}
		if err := json.Unmarshal(response, &data); err != nil {
			return nil, fmt.Errorf("error unmarshalling message: %v", err)
		}

		if data["op"] == "push" {
			var fileData IncomingPushMessage
			if err := json.Unmarshal(response, &fileData); err != nil {
				return nil, fmt.Errorf("error unmarshalling push message: %v", err)
			}
			pushedFiles = append(pushedFiles, fileData)
		} else if data["op"] == "ready" {
			var readyData struct {
				Op  string `json:"op"`
				Uid int64  `json:"version"`
			}
			if err := json.Unmarshal(response, &readyData); err != nil {
				return nil, fmt.Errorf("error unmarshalling ready message: %v", err)
			}
			remoteUid = readyData.Uid
			break
		} else {
			return nil, fmt.Errorf("expected push or ready message, got %s", response)
		}
	}

	return &InitResult{
		RemoteUid:   remoteUid,
		PushedFiles: pushedFiles,
	}, nil
}

// GetSizeConfig returns the size and limit of the vault
func (ctx *ObsidianSocketContext) GetSizeConfig() (int64, int64, error) {
	// send size op
	if err := ctx.sendMessage(struct {
		Op string `json:"op"`
	}{
		Op: "size",
	}); err != nil {
		return 0, 0, fmt.Errorf("could not send size message: %v", err)
	}

	// Next message should be a size response
	response, err := ctx.nextMessageWithJsonKeys("size", "limit")
	if err != nil {
		return 0, 0, fmt.Errorf("error reading size message: %v", err)
	}

	type SizeResponse struct {
		Size  int64 `json:"size"`
		Limit int64 `json:"limit"`
	}
	var sizeResponse SizeResponse
	if err := json.Unmarshal(response, &sizeResponse); err != nil {
		return 0, 0, fmt.Errorf("error unmarshalling size message: %v", err)
	}

	return sizeResponse.Size, sizeResponse.Limit, nil
}

// PullFile initiates a pull for a file, which should send a header and binary data
// TODO: This should also support a deletion result
func (ctx *ObsidianSocketContext) PullFile(uid int64, expectedEncryptedHash string) ([]byte, error) {
	// send a pull op for this UID
	pullMsg := struct {
		Op  string `json:"op"`
		UID int64  `json:"uid"`
	}{
		Op:  "pull",
		UID: uid,
	}

	if err := ctx.sendMessage(pullMsg); err != nil {
		return nil, fmt.Errorf("could not send pull message: %v", err)
	}

	// Next message should be a header
	header, err := ctx.nextMessageWithJsonKeys("hash", "size", "pieces")
	if err != nil {
		return nil, fmt.Errorf("error reading header: %v", err)
	}

	// Unmarshal pull header message
	var headerMessage PullHeaderMessage
	err = json.Unmarshal(header, &headerMessage)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal pull header message: %v", err)
	}

	fmt.Printf("ℹ️ Received header for %d\n", uid)
	fmt.Printf("ℹ️ Size: %d\n", headerMessage.Size)
	fmt.Printf("ℹ️ Pieces: %d\n", headerMessage.Pieces)

	var data []byte
	// Intercept N websocket messages and append them to the session data
	for i := 0; i < headerMessage.Pieces; i++ {
		message, err := ctx.nextBinaryMessage()
		if err != nil {
			return nil, fmt.Errorf("error reading piece: %v", err)
		}
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

	// Decrypt the expected hash
	decryptedExpectedHash, err := crypto.DecryptString(expectedEncryptedHash, []byte(ctx.Vault.Password), []byte(ctx.Vault.Salt))
	if err != nil {
		return nil, fmt.Errorf("could not decrypt expected hash: %v", err)
	}

	// Decode the expected hash
	expectedHashBytes, err := hex.DecodeString(decryptedExpectedHash)
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
		Pieces:  1, // TODO: 2MB chunks
	}

	if err := ctx.sendMessage(message); err != nil {
		return fmt.Errorf("could not send push message: %v", err)
	}

	// Next message should be a {"res": "next"}
	response, err := ctx.nextMessageWithJsonValue("res", "next")
	if err != nil {
		return fmt.Errorf("error reading next response: %v", err)
	}
	var nextResponse struct {
		Res string `json:"res"`
	}
	if err := json.Unmarshal(response, &nextResponse); err != nil {
		return fmt.Errorf("could not unmarshal next response: %v", err)
	}
	if nextResponse.Res != "next" {
		return fmt.Errorf("next response is not 'next'")
	}

	// send the encrypted content
	if err := ctx.sendBinary(encryptedContent); err != nil {
		return fmt.Errorf("could not send encrypted content: %v", err)
	}

	// TODO: Loop this next+binary pair for each 2MB chunk of the file

	// Next message should be an incoming push
	response, err = ctx.nextMessageWithJsonValue("op", "push")
	if err != nil {
		return fmt.Errorf("error reading push response: %v", err)
	}
	var pushResponse IncomingPushMessage
	if err := json.Unmarshal(response, &pushResponse); err != nil {
		return fmt.Errorf("could not unmarshal push response: %v", err)
	}
	// TODO: Make sure the incoming push response matches the outgoing push message

	// Next message should be an {"op": "ok"}
	response, err = ctx.nextMessageWithJsonValue("op", "ok")
	if err != nil {
		return fmt.Errorf("error reading ok response: %v", err)
	}
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

func (ctx *ObsidianSocketContext) WaitForPushMessage() (*IncomingPushMessage, error) {
	// Send ping message every 20-30s until we get a push message then return
	resultChan := make(chan *IncomingPushMessage)
	errorChan := make(chan error)
	doneChan := make(chan bool)

	// Start pinging until we get a push message, then signal that we're done
	var unansweredPingCount = 0
	go func() {
		for {
			message, err := ctx.nextMessageMatchingJson(func(json map[string]interface{}) bool {
				return json["op"] == "push" || json["op"] == "pong"
			})
			if err != nil {
				errorChan <- fmt.Errorf("error reading message: %v", err)
			}
			var data map[string]interface{}
			if err := json.Unmarshal(message, &data); err != nil {
				errorChan <- fmt.Errorf("could not unmarshal message: %v", err)
			}
			if data["op"] == "push" {
				var pushMessage IncomingPushMessage
				if err := json.Unmarshal(message, &pushMessage); err != nil {
					errorChan <- fmt.Errorf("could not unmarshal push message: %v", err)
				}
				resultChan <- &pushMessage

				// Stop listening if we've got a push and no unanswered pings
				if unansweredPingCount == 0 {
					doneChan <- true
					return
				}
			} else if data["op"] == "pong" {
				unansweredPingCount--
			} else {
				errorChan <- fmt.Errorf("unexpected message: %v", string(message))
			}
		}
	}()

	// Ping until done
	go func() {
		for {
			secs := rand.Intn(10) + 20
			select {
			case <-time.After(time.Duration(secs) * time.Second):
				if err := ctx.sendMessage(struct {
					Op string `json:"op"`
				}{
					Op: "ping",
				}); err != nil {
					errorChan <- fmt.Errorf("could not send ping message: %v", err)
				}
				unansweredPingCount++
			case <-doneChan:
				return
			}
		}
	}()

	select {
	case result := <-resultChan:
		return result, nil
	case err := <-errorChan:
		return nil, err
	}
}
