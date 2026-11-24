package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is overridden at build time via -ldflags:
//
//	go build -ldflags "-X github.com/openshift/cluster-health-analyzer/cmd.Version=$(git describe --tags --always --dirty)"
var Version = "dev"

var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version and build type",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Printf("cluster-health-analyzer %s (%s build)\n", Version, buildFlavor)
	},
}
