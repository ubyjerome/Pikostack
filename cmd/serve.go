package cmd

import (
	"fmt"
	"log"

	"github.com/pikostack/pikostack/internal/api"
	"github.com/pikostack/pikostack/internal/config"
	"github.com/pikostack/pikostack/internal/db"
	"github.com/pikostack/pikostack/internal/monitor"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Pikoview web server and monitoring engine",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()

		database, err := db.Init(cfg.Database.Path)
		if err != nil {
			return fmt.Errorf("database init: %w", err)
		}

		mon := monitor.New(database, cfg)
		mon.Start()

		router := api.NewRouter(database, mon, cfg)
		addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
		log.Printf("Pikoview listening on http://%s", addr)
		return router.Run(addr)
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Pikostack v0.1.0")
	},
}

func init() {
	serveCmd.Flags().StringP("host", "H", "0.0.0.0", "bind host")
	serveCmd.Flags().IntP("port", "p", 7331, "bind port")
	viper.BindPFlag("server.host", serveCmd.Flags().Lookup("host"))
	viper.BindPFlag("server.port", serveCmd.Flags().Lookup("port"))
}
