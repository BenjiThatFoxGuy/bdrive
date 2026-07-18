package cmd

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/internal/database"
	"github.com/tgdrive/teldrive/pkg/models"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type DedupStats struct {
	ProcessedUsers   int64
	DuplicateGroups  int64
	TotalFilesLinked int64
	SkippedFiles     int64
}

func NewDeduplicateCmd() *cobra.Command {
	var cfg config.CheckCmdConfig
	loader := config.NewConfigLoader()

	cmd := &cobra.Command{
		Use:   "deduplicate",
		Short: "Retroactively deduplicate existing files based on content hash",
		Long: `Retroactively deduplicate existing files based on content hash.
		
This tool groups files by their content hash and sets up copy-on-write references
for duplicate files. Only non-encrypted files are deduplicated.

Examples:
  # Dry-run: show what would be deduplicated without making changes
  teldrive deduplicate --dry-run

  # Deduplicate specific user
  teldrive deduplicate --user alice

  # Deduplicate all users (requires confirmation)
  teldrive deduplicate --all
		`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if err := loader.Load(cmd, &cfg); err != nil {
				return err
			}
			if cfg.DB.DataSource == "" {
				return fmt.Errorf("database connection required: set --db-data-source or config file")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			runDeduplication(cmd, &cfg)
		},
	}

	loader.RegisterFlags(cmd.Flags(), reflect.TypeFor[config.CheckCmdConfig]())

	return cmd
}

func runDeduplication(cmd *cobra.Command, cfg *config.CheckCmdConfig) {
	ctx := cmd.Context()

	dryRun := cfg.DryRun
	userName := cfg.User

	logCfg := &config.DBLoggingConfig{
		Level: "fatal",
	}
	db, err := database.NewDatabase(ctx, &cfg.DB, logCfg, zap.NewNop())
	if err != nil {
		color.Red("Failed to connect to database: %v\n", err)
		os.Exit(1)
	}

	// If no user specified, ask whether to process all or specific user
	if userName == "" {
		fmt.Print("\nDeduplicate which user(s)?\n")
		fmt.Print("  1. Specific user\n")
		fmt.Print("  2. All users\n")
		fmt.Print("Select (1 or 2): ")
		var choice string
		fmt.Scanln(&choice)

		if choice == "1" {
			// Get list of users and let them choose
			var users []models.User
			if err := db.Model(&models.User{}).Find(&users).Error; err != nil {
				color.Red("Failed to retrieve users: %v\n", err)
				os.Exit(1)
			}

			if len(users) == 0 {
				color.Red("No users found in database\n")
				os.Exit(1)
			}

			fmt.Println("\nAvailable users:")
			for i, u := range users {
				fmt.Printf("  %d. %s (ID: %d)\n", i+1, u.UserName, u.UserId)
			}
			fmt.Print("Select user (number): ")
			var selection string
			fmt.Scanln(&selection)

			// Parse selection
			var idx int
			_, err := fmt.Sscanf(selection, "%d", &idx)
			if err != nil || idx < 1 || idx > len(users) {
				color.Red("Invalid selection\n")
				os.Exit(1)
			}

			deduplicateUser(ctx, db, users[idx-1].UserId, &DedupStats{}, dryRun)
		} else if choice == "2" {
			// Ask for confirmation
			if !dryRun {
				color.Yellow("\n⚠️  WARNING: This will deduplicate ALL users' files.\n")
				fmt.Print("Proceed? Type 'yes' to confirm: ")
				var input string
				fmt.Scanln(&input)
				if strings.ToLower(input) != "yes" {
					fmt.Println("Operation cancelled.")
					return
				}
			}
			deduplicateAllUsers(ctx, db, &DedupStats{}, dryRun)
		} else {
			color.Red("Invalid choice\n")
			os.Exit(1)
		}
	} else {
		// User specified
		var user models.User
		if err := db.Model(&models.User{}).Where("user_name = ?", userName).First(&user).Error; err != nil {
			color.Red("User not found: %s\n", userName)
			os.Exit(1)
		}
		deduplicateUser(ctx, db, user.UserId, &DedupStats{}, dryRun)
	}
}

func deduplicateUser(ctx context.Context, db *gorm.DB, userID int64, stats *DedupStats, dryRun bool) {
	color.Cyan("Starting deduplication for user %d...\n", userID)

	// Find all files with non-null, non-empty hash for this user (non-encrypted only)
	var files []models.File
	if err := db.Where(
		"user_id = ? AND hash IS NOT NULL AND hash != '' AND encrypted = false AND status = 'active'",
		userID,
	).Find(&files).Error; err != nil {
		color.Red("Failed to query files: %v\n", err)
		return
	}

	if len(files) == 0 {
		color.Yellow("No files to deduplicate for user %d\n", userID)
		return
	}

	color.Cyan("Found %d files to process for user %d\n", len(files), userID)

	// Group files by hash
	hashGroups := make(map[string][]models.File)
	for _, file := range files {
		if file.Hash != nil && *file.Hash != "" {
			hashGroups[*file.Hash] = append(hashGroups[*file.Hash], file)
		}
	}

	// Process each hash group with more than one file
	for hash, group := range hashGroups {
		if len(group) <= 1 {
			continue // Skip if only one file with this hash
		}

		stats.DuplicateGroups++

		// First file is the canonical one
		canonical := group[0]
		color.Cyan("Found duplicate group: hash=%s count=%d canonical=%s (%s)\n",
			hash[:16]+"...", len(group), canonical.ID, canonical.Name)

		// Link all other files to the canonical one
		for i := 1; i < len(group); i++ {
			duplicate := group[i]

			if !dryRun {
				// Update the duplicate file to reference the canonical file
				if err := db.Model(&models.File{}).
					Where("id = ?", duplicate.ID).
					Updates(map[string]interface{}{
						"referenced_file_id": canonical.ID,
						"parts":              canonical.Parts,
						"channel_id":         canonical.ChannelId,
					}).Error; err != nil {
					color.Red("  ✗ Failed to update %s: %v\n", duplicate.ID, err)
					continue
				}
			}

			stats.TotalFilesLinked++
			color.Green("  ✓ Linked: %s (%s)\n", duplicate.ID, duplicate.Name)
		}
	}

	stats.ProcessedUsers++
}

func deduplicateAllUsers(ctx context.Context, db *gorm.DB, stats *DedupStats, dryRun bool) {
	color.Cyan("Starting deduplication for all users...\n")

	// Find all distinct users that have files with hashes
	var userIDs []int64
	if err := db.Model(&models.File{}).
		Where("hash IS NOT NULL AND hash != '' AND encrypted = false AND status = 'active'").
		Distinct("user_id").
		Pluck("user_id", &userIDs).Error; err != nil {
		color.Red("Failed to query distinct users: %v\n", err)
		return
	}

	color.Cyan("Found %d users with files to deduplicate\n\n", len(userIDs))

	for _, uid := range userIDs {
		deduplicateUser(ctx, db, uid, stats, dryRun)
		fmt.Println()
	}
}

func printDedupStats(stats *DedupStats, dryRun bool) {
	modeStr := "DRY-RUN"
	if !dryRun {
		modeStr = "APPLIED"
	}

	fmt.Printf(`
╔════════════════════════════════════════╗
║   Deduplication Summary (%s)   ║
╠════════════════════════════════════════╣
║ Processed Users:      %10d        ║
║ Duplicate Groups:     %10d        ║
║ Total Files Linked:   %10d        ║
║ Skipped Files:        %10d        ║
╚════════════════════════════════════════╝
`, modeStr, stats.ProcessedUsers, stats.DuplicateGroups, stats.TotalFilesLinked, stats.SkippedFiles)
}
