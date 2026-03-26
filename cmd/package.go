package cmd

import (
	"fmt"
	"os"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

var (
	packageOutput    string
	packageBinaries  string
)

func init() {
	packageBuildCmd.Flags().StringVarP(&packageOutput, "output", "o", "kawarimi-vault.zip", "Output zip file path")
	packageBuildCmd.Flags().StringVar(&packageBinaries, "binaries", "", "Directory containing cross-compiled kawarimi binaries")

	packageCmd.AddCommand(packageBuildCmd)
	rootCmd.AddCommand(packageCmd)
}

var packageCmd = &cobra.Command{
	Use:   "package",
	Short: "Manage vault distribution packages",
}

var packageBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build a distributable vault package (zip) for recipients",
	Long: `Creates a zip file containing the encrypted vault and kawarimi binaries.

No secrets are included in the package. Recipients also need:
- The sealed payload (delivered by the dead man's switch)
- The recipient passphrase (from a physical card)

Use --binaries to include cross-compiled binaries:
  GOOS=linux GOARCH=amd64 go build -o dist/kawarimi-linux-amd64 .
  GOOS=darwin GOARCH=arm64 go build -o dist/kawarimi-darwin-arm64 .
  GOOS=windows GOARCH=amd64 go build -o dist/kawarimi-windows-amd64.exe .
  kawarimi package build --binaries ./dist/`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		// Verify vault exists
		if _, err := os.Stat(cfg.VaultDir); os.IsNotExist(err) {
			return fmt.Errorf("vault directory not found: %s", cfg.VaultDir)
		}

		if err := vault.BuildPackage(cfg.VaultDir, packageOutput, packageBinaries); err != nil {
			return fmt.Errorf("building package: %w", err)
		}

		info, err := os.Stat(packageOutput)
		if err != nil {
			return fmt.Errorf("stat output: %w", err)
		}

		fmt.Printf("Vault package created: %s (%.1f KB)\n", packageOutput, float64(info.Size())/1024)
		fmt.Println()
		fmt.Println("This package contains NO secrets. To decrypt, recipients need:")
		fmt.Println("  1. The sealed payload (delivered by the dead man's switch)")
		fmt.Println("  2. The recipient passphrase (from the physical card)")
		fmt.Println()
		fmt.Println("Upload this package to your chosen storage backend (Google Drive, GitHub, USB).")
		return nil
	},
}
