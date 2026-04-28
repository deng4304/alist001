package cmd

import (
	"github.com/alist-org/alist/v3/internal/bootstrap"
	mcpserver "github.com/alist-org/alist/v3/server/mcp"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/spf13/cobra"
)

var MCPCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server in STDIO mode",
	Long:  `Start an MCP (Model Context Protocol) server that communicates via STDIO, suitable for integration with AI assistants like Claude Desktop.`,
	Run: func(cmd *cobra.Command, args []string) {
		Init()
		bootstrap.LoadStorages()
		username, _ := cmd.Flags().GetString("user")
		if err := mcpserver.ServeStdio(username); err != nil {
			utils.Log.Fatalf("MCP STDIO server error: %v", err)
		}
	},
}

func init() {
	MCPCmd.Flags().String("user", "admin", "Username for MCP operations")
	RootCmd.AddCommand(MCPCmd)
}
