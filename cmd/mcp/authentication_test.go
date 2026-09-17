//go:build !testonly

package mcp

import "testing"

func TestMCPFlagsDoNotExposeDisableAuthForTesting(t *testing.T) {
	if flag := MCPCmd.Flags().Lookup("disable-auth-for-testing"); flag != nil {
		t.Fatalf("test-only authentication bypass must not be exposed: %q", flag.Name)
	}

	if newMCPHealthServerConfig().DisableAuthForTesting {
		t.Fatal("production MCP server configuration must enable token authentication")
	}
}
