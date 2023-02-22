package cmd

import (
	"fmt"
	"github.com/nbadal/obsidian-sync/api"
	"github.com/nbadal/obsidian-sync/sync"
	"github.com/spf13/cobra"
	"strconv"
)

func init() {
	syncCmd.Flags().StringP("vaultId", "v", "", "Vault ID to sync")
	syncCmd.Flags().StringP("password", "p", "", "Password to decrypt vault")
	syncCmd.Flags().StringP("authToken", "t", "", "Auth token to use")
	syncCmd.Flags().BoolP("daemon", "d", false, "Run as a daemon, continuously syncing in the background")
	rootCmd.AddCommand(syncCmd)
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync local files with the cloud",
	Long:  "Sync local files with the cloud, uploading local changes and downloading remote changes",
	Run: func(cmd *cobra.Command, args []string) {
		// Get flags
		vault, _ := cmd.Flags().GetString("vault")
		password, _ := cmd.Flags().GetString("password")
		authToken, _ := cmd.Flags().GetString("authToken")
		daemon, _ := cmd.Flags().GetBool("daemon")

		err := promptForNeededInfoThenSync(authToken, vault, password, daemon)
		if err != nil {
			fmt.Printf("Error syncing: %s\n", err)
			return
		}
	},
}

func promptForNeededInfoThenSync(authToken, vaultId, password string, daemon bool) error {
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

	// Sync
	err := sync.Sync(authToken, vaultInfo, password, daemon)
	if err != nil {
		return fmt.Errorf("error syncing: %s", err)
	}
	return nil
}
