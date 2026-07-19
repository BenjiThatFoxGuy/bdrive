package cmd

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/internal/database"
	"github.com/tgdrive/teldrive/internal/tgc"
	"github.com/tgdrive/teldrive/pkg/models"
	"github.com/tgdrive/teldrive/pkg/services"
	"go.uber.org/zap"
)

// backfillCacheSize is the in-memory cache size used by the standalone
// deduplicate command when re-reading file content from Telegram to backfill
// a hash. This is a one-shot admin job, so a small local cache (no Redis) is
// sufficient.
const backfillCacheSize = 10 * 1024 * 1024

func NewDeduplicateCmd() *cobra.Command {
	var cfg config.DeduplicateCmdConfig
	loader := config.NewConfigLoader()

	cmd := &cobra.Command{
		Use:   "deduplicate",
		Short: "Retroactively deduplicate existing files based on content hash",
		Long: `Retroactively deduplicate existing files based on content hash.

This tool groups files by their content hash and sets up copy-on-write references
for duplicate files. Only non-encrypted files are deduplicated.

Files that don't have a hash yet - because they predate the dedup feature, or
were created through a path that doesn't compute one - are invisible to grouping
by default. Pass --backfill to compute a hash for those files first (by re-reading
their content back from Telegram), so they can be grouped and deduplicated too.

Examples:
  # Dry-run: show what would be deduplicated without making changes
  teldrive deduplicate --dry-run

  # Also backfill hashes for legacy files before grouping
  teldrive deduplicate --backfill --user alice

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

	loader.RegisterFlags(cmd.Flags(), reflect.TypeFor[config.DeduplicateCmdConfig]())

	return cmd
}

func runDeduplication(cmd *cobra.Command, cfg *config.DeduplicateCmdConfig) {
	ctx := cmd.Context()

	dryRun := cfg.DryRun
	userName := cfg.User
	backfill := cfg.Backfill

	logCfg := &config.DBLoggingConfig{
		Level: "fatal",
	}
	db, err := database.NewDatabase(ctx, &cfg.DB, logCfg, zap.NewNop())
	if err != nil {
		color.Red("Failed to connect to database: %v\n", err)
		os.Exit(1)
	}

	// The dedup logic is shared with the async web-UI job (see
	// services.RunDedupForUser); the CLI supplies an in-memory cache and bot
	// selector since it's a one-shot process without the running server's Redis.
	cacher := cache.NewMemoryCache(backfillCacheSize)
	deps := services.DedupDeps{
		DB:             db,
		Cache:          cacher,
		TG:             &cfg.TG,
		ChannelManager: tgc.NewChannelManager(db, cacher, &cfg.TG),
		BotSelector:    tgc.NewMemoryBotSelector(),
	}

	stats := &api.DedupStats{}

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

			deduplicateUser(ctx, deps, users[idx-1].UserId, stats, dryRun, backfill)
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
			deduplicateAllUsers(ctx, deps, stats, dryRun, backfill)
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
		deduplicateUser(ctx, deps, user.UserId, stats, dryRun, backfill)
	}

	printDedupStats(stats, dryRun)
}

// cliDedupHooks builds the progress/logging hooks for a CLI dedup run: it keeps
// the detailed colored per-file lines and adds `teldrive check`-style aggregate
// phase progress (throttled to ~10 checkpoints so it doesn't drown the per-file
// output).
func cliDedupHooks() services.DedupHooks {
	return services.DedupHooks{
		OnPhase: func(phase string, total int64) {
			switch phase {
			case string(api.DedupPhaseBackfilling):
				if total > 0 {
					color.Cyan("Backfilling hash for %d file(s)...\n", total)
				}
			case string(api.DedupPhaseLinking):
				color.Cyan("Linking %d duplicate(s)...\n", total)
			}
		},
		OnProgress: func(phase string, current, total int64) {
			if total <= 0 {
				return
			}
			step := total / 10
			if step < 1 {
				step = 1
			}
			if current == total || current%step == 0 {
				percent := float64(current) / float64(total) * 100
				color.New(color.FgYellow).Printf("[dedup] %s: %d/%d (%.1f%%)\n", phase, current, total, percent)
			}
		},
		OnGroup: func(hash string, count int, canonical models.File) {
			color.Cyan("Found duplicate group: hash=%s count=%d canonical=%s (%s)\n",
				hash[:16]+"...", count, canonical.ID, canonical.Name)
		},
		OnLinked: func(dup, canonical models.File) {
			color.Green("  ✓ Linked: %s (%s)\n", dup.ID, dup.Name)
		},
		OnBackfilled: func(file models.File) {
			color.Green("  ✓ Backfilled hash: %s (%s)\n", file.ID, file.Name)
		},
		OnSkipped: func(file models.File, err error) {
			color.Red("  ✗ Skipped %s (%s): %v\n", file.ID, file.Name, err)
		},
	}
}

func deduplicateUser(ctx context.Context, deps services.DedupDeps, userID int64, stats *api.DedupStats, dryRun bool, backfill bool) {
	color.Cyan("Starting deduplication for user %d...\n", userID)

	if err := services.RunDedupForUser(ctx, deps, userID, dryRun, backfill, stats, cliDedupHooks()); err != nil {
		color.Red("Deduplication failed for user %d: %v\n", userID, err)
		return
	}

	stats.ProcessedUsers++
}

func deduplicateAllUsers(ctx context.Context, deps services.DedupDeps, stats *api.DedupStats, dryRun bool, backfill bool) {
	color.Cyan("Starting deduplication for all users...\n")

	// Find all distinct users that have files with hashes (or, with --backfill,
	// files that are simply eligible for one)
	hashCondition := "hash IS NOT NULL AND hash != ''"
	if backfill {
		hashCondition = "(hash IS NOT NULL AND hash != '') OR hash IS NULL"
	}

	var userIDs []int64
	if err := deps.DB.Model(&models.File{}).
		Where(hashCondition+" AND encrypted = false AND status = 'active' AND type = 'file'").
		Distinct("user_id").
		Pluck("user_id", &userIDs).Error; err != nil {
		color.Red("Failed to query distinct users: %v\n", err)
		return
	}

	color.Cyan("Found %d users with files to deduplicate\n\n", len(userIDs))

	for _, uid := range userIDs {
		deduplicateUser(ctx, deps, uid, stats, dryRun, backfill)
		fmt.Println()
	}
}

func printDedupStats(stats *api.DedupStats, dryRun bool) {
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
║ Hashes Backfilled:    %10d        ║
║ Skipped Files:        %10d        ║
╚════════════════════════════════════════╝
`, modeStr, stats.ProcessedUsers, stats.DuplicateGroups, stats.TotalFilesLinked, stats.HashesBackfilled, stats.SkippedFiles)
}
