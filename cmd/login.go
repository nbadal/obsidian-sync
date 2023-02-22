package cmd

import (
	"fmt"
	"github.com/nbadal/obsidian-sync/auth"
	"github.com/spf13/cobra"
)

func init() {
	loginCmd.Flags().StringP("email", "e", "", "Obsidian Sync email address")
	loginCmd.Flags().StringP("password", "p", "", "Obsidian Sync password")
	loginCmd.Flags().StringP("token", "t", "", "Obsidian Sync password")
	rootCmd.AddCommand(loginCmd)
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to Obsidian API",
	Long:  "Login to Obsidian API using an auth token, or email + password",
	Run: func(cmd *cobra.Command, args []string) {
		// Get flags
		token, _ := cmd.Flags().GetString("token")
		email, _ := cmd.Flags().GetString("email")
		password, _ := cmd.Flags().GetString("password")

		getTokenIfNeededAndStore(token, email, password)
	},
}

func getTokenIfNeededAndStore(token, email, password string) {
	// Store token if provided. Ignore email and password.
	if token != "" {
		err := auth.StoreToken(token)
		if err != nil {
			fmt.Printf("Error storing token: %s\n", err)
		}
		return
	}

	// Prompt for email and password if needed
	if email == "" {
		promptFor("Email: ", &email)
	}
	if password == "" {
		promptFor("Password: ", &password)
	}

	// Login and store token
	token, err := auth.Login(email, password)
	if err != nil {
		fmt.Printf("Error logging in: %s\n", err)
		return
	}
	err = auth.StoreToken(token)
	if err != nil {
		fmt.Printf("Error storing token: %s\n", err)
		return
	}
}

func promptFor(prompt string, value *string) {
	fmt.Print(prompt)
	_, err := fmt.Scanln(value)
	if err != nil {
		fmt.Printf("Error reading input: %s\n", err)
		return
	}
}
