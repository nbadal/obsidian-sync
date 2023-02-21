package sync

import (
	"encoding/json"
	"fmt"
	"github.com/nbadal/obsidian-sync/api"
	"github.com/nbadal/obsidian-sync/crypto"
	"log"
	"os"
)

type ObsidianEntry struct {
	Path     string
	IsFolder bool
	Hash     string
	Uid      int64
	Created  int64
	Modified int64
}

type State struct {
	ReadyToSync bool
	SyncSignal  chan bool
	LocalFiles  map[string]ObsidianEntry
	RemoteFiles map[string]ObsidianEntry
}

func Sync(authToken string, vault api.VaultInfo, password string) error {
	syncState := State{
		ReadyToSync: false,
		SyncSignal:  make(chan bool),
		LocalFiles:  make(map[string]ObsidianEntry),
		RemoteFiles: make(map[string]ObsidianEntry),
	}

	handlers := api.MessageHandlers{}
	handlers.RegisterOpHandler("push", syncState.onPush)
	handlers.RegisterOpHandler("ready", syncState.onReady)
	handlers.RegisterNoOpHandler(func(ctx *api.ObsidianSocketContext, msg []byte) error {
		log.Print("Unexpected message without op: " + string(msg))
		return nil
	})
	handlers.RegisterBinaryHandler(func(*api.ObsidianSocketContext, []byte) error {
		panic("Unexpected binary message, this should have been consumed by the file puller")
	})

	// Create websocket API connection
	ctx, err := api.ConnectToVault(vault, password, authToken, &handlers)
	if err != nil {
		return fmt.Errorf("error connecting to vault: %s", err)
	}
	defer ctx.Close()

	// Send initial sync message
	err = ctx.SendInit()
	if err != nil {
		return fmt.Errorf("error sending init message: %s", err)
	}

	// Sync when ready
	go func() {
		for {
			// Wait for ready signal
			if !syncState.ReadyToSync {
				continue
			}
			// Each time we get a sync signal, do a sync
			<-syncState.SyncSignal
			err := syncState.DoSync(ctx)
			if err != nil {
				log.Fatalf("error syncing: %v", err)
			}
		}
	}()

	// Print log messages
	for {
		msg := <-ctx.MessageLog
		fmt.Printf("%s\n", msg)
	}
}

func (s *State) onPush(ctx *api.ObsidianSocketContext, msg []byte) error {
	// Unmarshal message
	pushMessage := api.IncomingPushMessage{}
	err := json.Unmarshal(msg, &pushMessage)
	if err != nil {
		return fmt.Errorf("could not unmarshal push message: %v", err)
	}

	// Decrypt Path
	decryptedPath, err := crypto.DecryptString(pushMessage.Path, []byte(ctx.Vault.Password), []byte(ctx.Vault.Salt))
	if err != nil {
		return fmt.Errorf("could not decrypt path: %v", err)
	}

	// Decrypt Hash
	decryptedHash, err := crypto.DecryptString(pushMessage.Hash, []byte(ctx.Vault.Password), []byte(ctx.Vault.Salt))
	if err != nil {
		return fmt.Errorf("could not decrypt hash: %v", err)
	}

	// Add to remote files
	s.RemoteFiles[decryptedPath] = ObsidianEntry{
		Path:     decryptedPath,
		Uid:      pushMessage.Uid,
		IsFolder: pushMessage.Folder,
		Hash:     decryptedHash,
	}

	if !pushMessage.Folder {
		ctx.MessageLog <- fmt.Sprintf("📄 Got push for file %s version %d", decryptedPath, pushMessage.Uid)
	} else {
		ctx.MessageLog <- fmt.Sprintf("📁 Got push for folder %s version %d", decryptedPath, pushMessage.Uid)
	}

	return nil
}

func (s *State) onReady(ctx *api.ObsidianSocketContext, msg []byte) error {
	// Unmarshal message
	readyMessage := api.ReadyMessage{}
	err := json.Unmarshal(msg, &readyMessage)
	if err != nil {
		log.Fatalf("could not unmarshal ready message: %v", err)
	}

	// Log that server is ready to receive pulls
	ctx.MessageLog <- fmt.Sprintf("🚀 Server is ready to receive pulls. Version: %d", readyMessage.Version)

	// Request size parameters
	ctx.MessageLog <- "📊 Requesting size parameters..."
	err = ctx.SendAndHandleSizeOp()
	if err != nil {
		return fmt.Errorf("error sending size parameters: %s", err)
	}

	// Mark ready to sync, and queue a sync
	ctx.MessageLog <- "🔄 Starting sync..."
	s.ReadyToSync = true
	s.QueueSync()

	return nil
}

func (s *State) QueueSync() {
	if s.ReadyToSync {
		s.SyncSignal <- true
	}
}

// DoSync starts the sync process by pulling all files that are newer than the local version
func (s *State) DoSync(ws *api.ObsidianSocketContext) error {
	var pullPaths []string
	var pushPaths []string
	var conflictPaths []string

	// Pull any files that are newer on the server
	for path, remoteFile := range s.RemoteFiles {
		localFile, inLocal := s.LocalFiles[path]
		if !inLocal || (localFile.Uid < remoteFile.Uid && localFile.Modified < remoteFile.Modified) {
			pullPaths = append(pullPaths, path)
		}

		// Check for conflicts
		if inLocal && localFile.Uid < remoteFile.Uid && localFile.Modified > remoteFile.Modified {
			conflictPaths = append(conflictPaths, path)
		}
	}

	// Push any files that are newer on the client
	for path, localFile := range s.LocalFiles {
		remoteFile, inRemote := s.RemoteFiles[path]
		if !inRemote || (localFile.Uid > remoteFile.Uid && localFile.Modified > remoteFile.Modified) {
			pushPaths = append(pushPaths, path)
		}
	}

	// Print out summary
	ws.MessageLog <- fmt.Sprintf("📄 %d conflicts", len(conflictPaths))
	ws.MessageLog <- fmt.Sprintf("📄 %d files to push", len(pushPaths))
	ws.MessageLog <- fmt.Sprintf("📄 %d files to pull", len(pullPaths))

	// Pull conflicting data to compare
	for range conflictPaths {
		// TODO: Prompt user to choose which version to keep. Pulling if necessary
	}

	// Pull files
	for _, path := range pullPaths {
		pullEntry := s.RemoteFiles[path]
		ws.MessageLog <- fmt.Sprintf("📄 Pulling file %s version %d", path, pullEntry.Uid)
		content, err := ws.PullFile(pullEntry.Uid, pullEntry.Hash)
		if err != nil {
			return fmt.Errorf("error pulling file: %s", err)
		}

		// Print file contents
		ws.MessageLog <- fmt.Sprintf("%s", string(content))

		// Write file to disk
		//err = os.WriteFile(path, content, 0644)
		//if err != nil {
		//	return fmt.Errorf("error writing file to disk: %s", err)
		//}
	}

	// Push files
	for _, path := range pushPaths {
		pushEntry := s.LocalFiles[path]
		ws.MessageLog <- fmt.Sprintf("📄 Pushing file %s version %d", path, pushEntry.Uid)

		// Read file from disk
		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("error reading file from disk: %s", err)
		}

		// Push file
		err = ws.PushFile(pushEntry.Path, "md", pushEntry.Created, pushEntry.Modified, false, false, contents)
		if err != nil {
			return fmt.Errorf("error pushing file: %s", err)
		}
	}

	ws.MessageLog <- "🔄 Sync complete"

	return nil
}
