// Command mcp-contracts emits executable MCP schemas for contract maintenance.
package main

import (
	"0xbot.org/api/internal/mcpserver"
	"encoding/json"
	"os"
)

func main() {
	_, tools := mcpserver.Definitions()
	if err := json.NewEncoder(os.Stdout).Encode(tools); err != nil {
		panic(err)
	}
}
