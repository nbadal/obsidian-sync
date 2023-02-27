package cmd

import (
	"fmt"
	"github.com/nbadal/obsidian-sync/api"
	"github.com/nbadal/obsidian-sync/sync"
	"github.com/spf13/cobra"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

func init() {
	syncCmd.Flags().StringP("vaultId", "v", "", "Vault ID to sync")
	syncCmd.Flags().StringP("password", "p", "", "Password to decrypt vault")
	syncCmd.Flags().StringP("authToken", "t", "", "Auth token to use")
	syncCmd.Flags().BoolP("daemon", "d", false, "Run as a daemon, continuously syncing in the background")
	syncCmd.Flags().BoolP("force", "f", false, "Force sync, even if folder is not empty")
	syncCmd.Args = cobra.ExactArgs(1)
	rootCmd.AddCommand(syncCmd)
}

var syncCmd = &cobra.Command{
	Use:   "sync [target path]",
	Short: "Sync local files with the cloud",
	Long:  "Sync local files with the cloud, uploading local changes and downloading remote changes",
	Run: func(cmd *cobra.Command, args []string) {
		// Get flags
		vault, _ := cmd.Flags().GetString("vault")
		password, _ := cmd.Flags().GetString("password")
		authToken, _ := cmd.Flags().GetString("authToken")
		daemon, _ := cmd.Flags().GetBool("daemon")
		force, _ := cmd.Flags().GetBool("force")

		// Get args
		targetPath := args[0]
		err := validateFolder(&targetPath, force)
		if err != nil {
			fmt.Printf("Invalid target: %s\n", err)
			return
		}

		err = promptForNeededInfoThenSync(targetPath, authToken, vault, password, daemon)
		if err != nil {
			fmt.Printf("Error syncing: %s\n", err)
			return
		}
	},
}

func validateFolder(targetPath *string, skipEmptyCheck bool) error {
	// Expand path if it contains a tilde
	*targetPath = os.ExpandEnv(*targetPath)
	if strings.Contains(*targetPath, "~") {
		usr, err := user.Current()
		if err != nil {
			return fmt.Errorf("error getting current user: %s", err)
		}
		homeDir := usr.HomeDir
		*targetPath = strings.Replace(*targetPath, "~", homeDir, 1)
	}

	// Resolve target path to absolute path
	absPath, err := filepath.Abs(*targetPath)
	*targetPath = absPath
	if err != nil {
		return fmt.Errorf("error resolving target path: %s", err)
	}

	file, err := os.Open(*targetPath)
	if err != nil {
		return fmt.Errorf("error opening target path: %s", err)
	}

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("error getting file info: %s", err)
	}

	if !fileInfo.IsDir() {
		return fmt.Errorf("target path is not a folder")
	}

	if !skipEmptyCheck {
		// Check if folder is empty, and prompt for confirmation if not
		_, err = file.Readdirnames(1)
		if err != io.EOF {
			fmt.Printf("Warning: target folder is not empty. Existing files may be overwritten.\n")
			var confirm string
			promptFor("Continue? [y/N]: ", &confirm)
			if confirm != "y" && confirm != "Y" {
				return fmt.Errorf("user cancelled")
			}
		}
	}

	return nil
}

func promptForNeededInfoThenSync(targetPath, authToken, vaultId, password string, daemon bool) error {
	if authToken == "" {
		// TODO: Get auth token if needed from config file
		return fmt.Errorf("no auth token provided")
	}

	// Select vault if needed
	var vaultInfo api.VaultInfo
	if vaultId == "" {
		vaults, err := api.ListVaults(authToken)
		if err != nil {
			return fmt.Errorf("error listing vaults: %s", err)
		}

		// Print out vaults and prompt for selection and select vault info
		for i, vault := range vaults {
			fmt.Printf("%d. %s\n", i+1, vault.Name)
		}
		var vaultNum string
		promptFor("Select vault: ", &vaultNum)
		vaultNumInt, err := strconv.Atoi(vaultNum)
		if err != nil {
			return fmt.Errorf("error parsing vault number: %s", err)
		}
		if vaultNumInt < 1 || vaultNumInt > len(vaults) {
			return fmt.Errorf("invalid vault number")
		}
		vaultInfo = vaults[vaultNumInt-1]
	} else {
		// Find vault info matching vault ID
		vaults, err := api.ListVaults(authToken)
		if err != nil {
			return fmt.Errorf("error listing vaults: %s", err)
		}
		var vaultFound = false
		for _, v := range vaults {
			if v.Id == vaultId {
				vaultInfo = v
				vaultFound = true
				break
			}
		}
		if !vaultFound {
			return fmt.Errorf("vault not found")
		}
	}

	// Use password if set in vault info, otherwise prompt
	if password == "" {
		if vaultInfo.Password != "" {
			password = vaultInfo.Password
		} else {
			promptFor("Vault Password: ", &password)
		}
	}
	vaultInfo.Password = password

	// Sync
	err := sync.Sync(targetPath, authToken, vaultInfo, password, daemon)
	if err != nil {
		return fmt.Errorf("error syncing: %s", err)
	}
	return nil
}
