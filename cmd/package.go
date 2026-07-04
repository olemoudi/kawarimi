package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

var (
	packageOutput     string
	packageBinaries   string
	packageSource     string
	packageNoBinaries bool
)

func init() {
	packageBuildCmd.Flags().StringVarP(&packageOutput, "output", "o", "kawarimi-vault.zip", "Output zip file path")
	packageBuildCmd.Flags().StringVar(&packageBinaries, "binaries", "", "Directory of pre-built kawarimi binaries (overrides auto cross-compile)")
	packageBuildCmd.Flags().StringVar(&packageSource, "source", "", "Path to the kawarimi source checkout (for auto cross-compile)")
	packageBuildCmd.Flags().BoolVar(&packageNoBinaries, "no-binaries", false, "Skip binaries entirely (recipient must obtain kawarimi separately)")

	packageCmd.AddCommand(packageBuildCmd)
	rootCmd.AddCommand(packageCmd)
}

var packageCmd = &cobra.Command{
	Use:   "package",
	Short: "Manage vault distribution packages",
}

var packageBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build a self-contained vault package (zip) for recipients",
	Long: `Creates a zip containing the encrypted vault, the kawarimi program for each
platform, and bilingual INSTRUCTIONS.md. No secrets are included.

By default the binaries are cross-compiled automatically from the kawarimi source
(run this from a source checkout, or pass --source). Use --binaries to supply
pre-built binaries instead, or --no-binaries to build a package without them.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		if _, err := os.Stat(cfg.VaultDir); os.IsNotExist(err) {
			return fmt.Errorf("vault directory not found: %s", cfg.VaultDir)
		}

		binariesDir, cleanup, err := resolvePackageBinaries()
		if err != nil {
			return err
		}
		if cleanup != nil {
			defer cleanup()
		}

		if err := vault.BuildPackage(cfg.VaultDir, packageOutput, binariesDir); err != nil {
			return fmt.Errorf("building package: %w", err)
		}

		info, err := os.Stat(packageOutput)
		if err != nil {
			return fmt.Errorf("stat output: %w", err)
		}

		fmt.Printf("Vault package created: %s (%.1f MB)\n", packageOutput, float64(info.Size())/(1024*1024))
		fmt.Println()
		fmt.Println("This package contains NO secrets. To open it, recipients need:")
		fmt.Println("  1. The DMS key (delivered by email when the switch triggers)")
		fmt.Println("  2. The recipient passphrase (from the physical card)")
		fmt.Println()
		fmt.Println("Upload it to your chosen storage (Google Drive, GitHub release, USB) and set")
		fmt.Println("that location as VAULT_PACKAGE_LOCATION in the DMS repo.")
		return nil
	},
}

// resolvePackageBinaries decides where the packaged binaries come from and returns
// the directory to hand to BuildPackage plus an optional cleanup func. It fails loud
// rather than silently shipping a package whose instructions reference binaries that
// are not there.
func resolvePackageBinaries() (dir string, cleanup func(), err error) {
	switch {
	case packageNoBinaries:
		return "", nil, nil

	case packageBinaries != "":
		if len(kawarimiBinariesIn(packageBinaries)) == 0 {
			return "", nil, fmt.Errorf("--binaries dir %q contains no kawarimi-* binaries (use --no-binaries to skip)", packageBinaries)
		}
		return packageBinaries, nil, nil

	default:
		src := resolveSourceDir(packageSource)
		if src == "" {
			return "", nil, fmt.Errorf(
				"no kawarimi source found to build recipient binaries.\n" +
					"Run from a kawarimi source checkout, or pass --source <dir>, or --binaries <dir>\n" +
					"with pre-built binaries, or --no-binaries to skip (recipients must then obtain\n" +
					"kawarimi themselves)")
		}
		fmt.Println("Cross-compiling kawarimi for recipients (this may take a moment)...")
		tmp, err := os.MkdirTemp("", "kawarimi-bin-")
		if err != nil {
			return "", nil, err
		}
		built, err := vault.CrossCompile(src, tmp, version)
		if err != nil {
			os.RemoveAll(tmp)
			return "", nil, fmt.Errorf("cross-compiling: %w", err)
		}
		fmt.Printf("Built %d recipient binaries.\n", len(built))
		return tmp, func() { os.RemoveAll(tmp) }, nil
	}
}

// resolveSourceDir returns the kawarimi module source directory, or "" if none.
func resolveSourceDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if cwd, err := os.Getwd(); err == nil && isKawarimiModule(cwd) {
		return cwd
	}
	return ""
}

// isKawarimiModule reports whether dir is the root of the kawarimi Go module.
func isKawarimiModule(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	return err == nil && strings.Contains(string(data), "module github.com/olemoudi/kawarimi")
}

func kawarimiBinariesIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "kawarimi") {
			names = append(names, e.Name())
		}
	}
	return names
}
