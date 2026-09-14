//go:build testonly

package mcp

import "testing"

func TestMCPFlagsEnableTestOnlyAuthenticationBypass(t *testing.T) {
	previousDisableAuthForTesting := disableAuthForTesting
	disableAuthForTesting = false
	t.Cleanup(func() { disableAuthForTesting = previousDisableAuthForTesting })

	flags := MCPCmd.Flags()
	if flag := flags.Lookup("disable-auth-for-testing"); flag == nil {
		t.Fatal("test-only authentication bypass must be available in a testonly build")
	}
	if err := flags.Parse([]string{"--disable-auth-for-testing"}); err != nil {
		t.Fatalf("failed to parse test-only authentication bypass flag: %v", err)
	}

	if !newMCPHealthServerConfig().DisableAuthForTesting {
		t.Fatal("test-only bypass must disable MCP token authentication")
	}
}
