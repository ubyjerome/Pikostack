package cmd

import (
	"fmt"

	"github.com/pikostack/pikostack/internal/config"
	"github.com/pikostack/pikostack/internal/db"
	"github.com/pikostack/pikostack/internal/monitor"
	"github.com/pikostack/pikostack/internal/tui"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the terminal UI",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()

		database, err := db.Init(cfg.Database.Path)
		if err != nil {
			return fmt.Errorf("database init: %w", err)
		}

		mon := monitor.New(database, cfg)
		mon.Start()

		return tui.Run(database, mon, cfg)
	},
}
