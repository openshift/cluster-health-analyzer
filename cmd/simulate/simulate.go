package simulate

import (
	"github.com/spf13/cobra"

	sim "github.com/openshift/cluster-health-analyzer/pkg/simulate"
)

var outputFile = "cluster-health-analyzer-openmetrics.txt"
var scenarioFile string
var alertsOnly bool

var SimulateCmd = &cobra.Command{
	Use:   "simulate",
	Short: "Generate simulated data in openmetrics format",
	RunE: func(cmd *cobra.Command, args []string) error {
		return sim.Simulate(cmd.Context(), outputFile, scenarioFile, alertsOnly)
	},
}

func init() {
	SimulateCmd.Flags().StringVarP(&outputFile, "output", "o", outputFile, "output file")
	SimulateCmd.Flags().StringVarP(&scenarioFile, "scenario", "s", "", "CSV file with the scenario to simulate")
	SimulateCmd.Flags().BoolVar(&alertsOnly, "alerts-only", false, "Only output ALERTS metrics (for integration testing)")
}
