package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "pikostack",
	Short: "Pikostack — single-binary VPS service management",
	Long: `Pikostack is a self-hosted service management platform.
Run services, monitor uptime, auto-restart on failure, and manage
everything from a TUI or the Pikoview web dashboard.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: pikostack.yaml)")
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(tuiCmd)
	rootCmd.AddCommand(versionCmd)
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, _ := os.UserHomeDir()
		viper.AddConfigPath(".")
		viper.AddConfigPath(home + "/.config/pikostack")
		viper.AddConfigPath("/etc/pikostack")
		viper.SetConfigName("pikostack")
		viper.SetConfigType("yaml")
	}
	viper.AutomaticEnv()
	viper.SetEnvPrefix("PIKO")

	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("using config:", viper.ConfigFileUsed())
	}
}
