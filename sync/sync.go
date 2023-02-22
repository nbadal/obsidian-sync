package sync

import (
	"fmt"
	"github.com/nbadal/obsidian-sync/api"
	"github.com/nbadal/obsidian-sync/crypto"
	"os"
	"time"
)

type ObsidianRemoteEntry struct {
	Uid           int64
	EncryptedPath string
	EncryptedHash string
	Created       int64
	Modified      int64
	IsFolder      bool
}

type ObsidianLocalEntry struct {
	Path     string
	Created  int64
	Modified int64
	IsFolder bool
}

type State struct {
	LocalFiles    map[string]ObsidianLocalEntry
	RemoteEntries map[string]ObsidianRemoteEntry
	LastSync      int64
	Size          int64
	Limit         int64
}

func Sync(authToken string, vault api.VaultInfo, password string, daemon bool) error {
	// Create websocket API connection
	ctx, err := api.ConnectToVault(vault, password, authToken)
	if err != nil {
		return fmt.Errorf("error connecting to vault: %s", err)
	}
	defer ctx.Close()

	// send initial sync message
	fmt.Println("🔄 Initializing...")
	initResult, err := ctx.SendInit()
	if err != nil {
		return fmt.Errorf("error sending init message: %s", err)
	}
	fmt.Println("✅ Initialized")
	fmt.Printf("Got %d files from server\n", len(initResult.PushedFiles))

	// Get size info
	fmt.Println("📊 Getting size info...")
	size, limit, err := ctx.GetSizeConfig()

	// Create sync state
	syncState := State{
		LocalFiles:    make(map[string]ObsidianLocalEntry),
		RemoteEntries: make(map[string]ObsidianRemoteEntry),
		Size:          size,
		Limit:         limit,
	}
	for _, push := range initResult.PushedFiles {
		syncState.UpdateWithPush(&push)
	}

	// Do initial sync
	err = syncState.SyncFiles(ctx)
	if err != nil {
		return fmt.Errorf("error syncing files: %s", err)
	}

	// Start daemon if needed
	if daemon {
		fmt.Println("👻 Starting daemon...")
		err := syncState.StartDaemon(ctx)
		if err != nil {
			return fmt.Errorf("error starting daemon: %s", err)
		}
	}

	return nil
}

// TODO: Maybe batch syncs? Maybe debounce?
// TODO: Cache file hashes for moves so we don't redownload

// SyncFiles starts the sync process by pulling all files that are newer than the local version
func (s *State) SyncFiles(ws *api.ObsidianSocketContext) error {
	var pullPaths []string
	var pushPaths []string
	var newFolderPaths []string
	var deletePaths []string
	var conflictPaths []string

	// Pull any files that are newer on the server
	for path, remoteFile := range s.RemoteEntries {
		localFile, inLocal := s.LocalFiles[path]

		if inLocal {
			// If type changed, delete and pull or create folder
			if localFile.IsFolder != remoteFile.IsFolder {
				deletePaths = append(deletePaths, path)
				if localFile.IsFolder {
					pullPaths = append(pullPaths, path)
				} else {
					newFolderPaths = append(newFolderPaths, path)
				}
			}

			if !localFile.IsFolder {
				// Pull if remote file is newer
				if localFile.Modified < remoteFile.Modified {
					pullPaths = append(pullPaths, path)
				}

				// Conflict if local file is newer
				if localFile.Modified > remoteFile.Modified {
					conflictPaths = append(conflictPaths, path)
				}
			} else {
				// We don't care about mismatches for folders
			}
		} else {
			// Pull the file or create a folder
			if remoteFile.IsFolder {
				newFolderPaths = append(newFolderPaths, path)
			} else {
				pullPaths = append(pullPaths, path)
			}
		}
	}

	// Push any files that are newer on the client
	for path, localFile := range s.LocalFiles {
		remoteFile, inRemote := s.RemoteEntries[path]
		if !inRemote {
			// Push if file is modified newer
			if localFile.Modified > remoteFile.Modified {
				pushPaths = append(pushPaths, path)
			}

			// Delete if file was created before last sync
			if localFile.Created < s.LastSync {
				deletePaths = append(deletePaths, path)
			}
		} else {
			// These cases handled above
		}
	}

	// Print out summary
	fmt.Printf("%d files to delete\n", len(deletePaths))
	fmt.Printf("%d conflicts\n", len(conflictPaths))
	fmt.Printf("%d files to push\n", len(pushPaths))
	fmt.Printf("%d files to pull\n", len(pullPaths))
	fmt.Printf("%d new folders\n", len(newFolderPaths))

	// Pull conflicting data to compare
	for _, path := range conflictPaths {
		// Decrypt path
		decryptedPath, err := crypto.DecryptString(path, []byte(ws.Vault.Password), []byte(ws.Vault.Salt))
		if err != nil {
			return fmt.Errorf("error decrypting path: %s", err)
		}

		fmt.Printf("⚠️ Conflict detected for %s\n", decryptedPath)
		// TODO: Prompt user to choose which version to keep. Pulling if necessary
	}

	// Delete any paths indicated first
	for _, path := range deletePaths {
		// Decrypt path
		decryptedPath, err := crypto.DecryptString(path, []byte(ws.Vault.Password), []byte(ws.Vault.Salt))
		if err != nil {
			return fmt.Errorf("error decrypting path: %s", err)
		}

		fmt.Printf("🗑️ Deleting %s\n", decryptedPath)

		// Delete from os
		//err := os.RemoveAll(decryptedPath)
		//if err != nil {
		//	return fmt.Errorf("error deleting file: %s", err)
		//}

		// Delete from local entries
		delete(s.LocalFiles, path)
	}

	// Create any needed folders
	for _, path := range newFolderPaths {
		// Decrypt path
		decryptedPath, err := crypto.DecryptString(path, []byte(ws.Vault.Password), []byte(ws.Vault.Salt))
		if err != nil {
			return fmt.Errorf("error decrypting path: %s", err)
		}

		fmt.Printf("📁 Creating folder %s\n", decryptedPath)

		// Create folder
		//err := os.MkdirAll(decryptedPath, 0755)
		//if err != nil {
		//	return fmt.Errorf("error creating folder: %s", err)
		//}

		// Add folder to local entries
		s.LocalFiles[path] = ObsidianLocalEntry{
			Path:     decryptedPath,
			IsFolder: true,
		}
	}

	// Pull files
	for _, path := range pullPaths {
		// Decrypt path
		decryptedPath, err := crypto.DecryptString(path, []byte(ws.Vault.Password), []byte(ws.Vault.Salt))
		if err != nil {
			return fmt.Errorf("error decrypting path: %s", err)
		}

		pullEntry := s.RemoteEntries[path]
		fmt.Printf("📄 Pulling file %s version %d\n", decryptedPath, pullEntry.Uid)
		content, err := ws.PullFile(pullEntry.Uid, pullEntry.EncryptedHash)
		if err != nil {
			return fmt.Errorf("error pulling file: %s", err)
		}

		// Print file contents
		fmt.Printf("📄 %s contents:\n", decryptedPath)
		fmt.Printf("%s\n", string(content))

		// Write file to disk
		//err = os.WriteFile(path, content, 0644)
		//if err != nil {
		//	return fmt.Errorf("error writing file to disk: %s", err)
		//}

		// Update local state
		s.LocalFiles[path] = ObsidianLocalEntry{
			Path:     decryptedPath,
			Created:  pullEntry.Created,
			Modified: pullEntry.Modified,
			IsFolder: pullEntry.IsFolder,
		}
	}

	// Push files
	for _, path := range pushPaths {
		pushEntry := s.LocalFiles[path]
		fmt.Printf("📄 Pushing file %s\n", path)

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

	// Set last sync to now in milliseconds
	s.LastSync = time.Now().UnixNano() / 1000000

	fmt.Printf("🔄 Sync complete at %d\n", s.LastSync)

	return nil
}

func (s *State) StartDaemon(ctx *api.ObsidianSocketContext) error {
	for {
		fmt.Println("👻 Waiting for push message...")
		pushMsg, err := ctx.WaitForPushMessage()
		if err != nil {
			return fmt.Errorf("error getting push message: %s", err)
		}
		fmt.Printf("📄 Got push message for UID %d\n", pushMsg.Uid)

		// Update remote files
		s.UpdateWithPush(pushMsg)

		err = s.SyncFiles(ctx)
		if err != nil {
			return fmt.Errorf("error syncing files: %s", err)
		}
	}
}

func (s *State) UpdateWithPush(push *api.IncomingPushMessage) {
	if push.Deleted {
		delete(s.RemoteEntries, push.EncryptedPath)
	} else {
		s.RemoteEntries[push.EncryptedPath] = ObsidianRemoteEntry{
			EncryptedPath: push.EncryptedPath,
			IsFolder:      push.Folder,
			EncryptedHash: push.EncryptedHash,
			Uid:           push.Uid,
			Created:       push.Ctime,
			Modified:      push.Mtime,
		}
	}
}
