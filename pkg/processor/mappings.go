package processor

// This file contains data used to map the signal to particular components.

import "regexp"

var (
	nodeAlerts []string = []string{
		"NodeClockNotSynchronising",
		"KubeNodeNotReady",
		"KubeNodeUnreachable",
		"NodeSystemSaturation",
		"NodeFilesystemSpaceFillingUp",
		"NodeFilesystemAlmostOutOfSpace",
		"NodeMemoryMajorPagesFaults",
		"NodeNetworkTransmitErrs",
		"NodeTextFileCollectorScrapeError",
		"NodeFilesystemFilesFillingUp",
		"NodeNetworkReceiveErrs",
		"NodeClockSkewDetected",
		"NodeFilesystemAlmostOutOfFiles",
		"NodeWithoutOVNKubeNodePodRunning",
		"InfraNodesNeedResizingSRE",
		"NodeHighNumberConntrackEntriesUsed",
		"NodeMemHigh",
		"NodeNetworkInterfaceFlapping",
		"NodeWithoutSDNPod",
		"NodeCpuHigh",
		"CriticalNodeNotReady",
		"NodeFileDescriptorLimit",
		// subset of MCO alerts https://github.com/openshift/machine-config-operator/blob/204767253e30608b5b7fd70ad1ace02ba1d64b46/install/0000_90_machine-config_01_prometheus-rules.yaml#L115
		"MCCPoolAlert",
		"MCCDrainError",
		"MCDRebootError",
		"MCDPivotError",
	}

	coreMatchers = []componentMatcher{
		{"etcd", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-etcd",
				"openshift-etcd-operator",
			}}}},
		{"kube-apiserver", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-kube-apiserver",
				"openshift-kube-apiserver-operator",
			}}}},
		{"kube-controller-manager", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-kube-controller-manager",
				"openshift-kube-controller-manager-operator",
				"kube-system",
			}}}},
		{"kube-scheduler", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-kube-scheduler",
				"openshift-kube-scheduler-operator",
			}}}},
		{"machine-approver", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-cluster-machine-approver",
				"openshift-machine-approver-operator",
			}}}},
		{"machine-config", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-machine-config-operator",
			}},
			labelMatcher{"alertname", stringMatcher{
				"HighOverallControlPlaneMemory",
				"ExtremelyHighIndividualControlPlaneMemory",
				"MissingMachineConfig",
				"MCCBootImageUpdateError",
				"KubeletHealthState",
				"SystemMemoryExceedsReservation",
			}}}},
		{"version", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-cluster-version",
				"openshift-version-operator",
			}},
			labelMatcher{"alertname", stringMatcher{
				"ClusterNotUpgradeable",
				"UpdateAvailable",
			}}}},
		{"dns", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-dns",
				"openshift-dns-operator",
			}}}},
		{"authentication", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-authentication",
				"openshift-oauth-apiserver",
				"openshift-authentication-operator",
			}}}},
		{"cert-manager", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-cert-manager",
				"openshift-cert-manager-operator",
			}}}},
		{"cloud-controller-manager", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-cloud-controller-manager",
				"openshift-cloud-controller-manager-operator",
			}}}},
		{"cloud-credential", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-cloud-credential-operator",
			}}}},
		{"cluster-api", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-cluster-api",
				"openshift-cluster-api-operator",
			}}}},
		{"config-operator", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-config-operator",
			}}}},
		{"kube-storage-version-migrator", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-kube-storage-version-migrator",
				"openshift-kube-storage-version-migrator-operator",
			}}}},
		{"image-registry", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-image-registry",
				"openshift-image-registry-operator",
			}}}},
		{"ingress", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-ingress",
				"openshift-route-controller-manager",
				"openshift-ingress-canary",
				"openshift-ingress-operator",
			}}}},
		{"console", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-console",
				"openshift-console-operator",
			}}}},
		{"insights", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-insights",
				"openshift-insights-operator",
			}}}},
		{"machine-api", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-machine-api",
				"openshift-machine-api-operator",
			}}}},
		{"monitoring", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-monitoring",
				"openshift-monitoring-operator",
			}}}},
		{"network", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-network-operator",
				"openshift-ovn-kubernetes",
				"openshift-multus",
				"openshift-network-diagnostics",
				"openshift-sdn",
			}}}},
		{"node-tuning", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-cluster-node-tuning-operator",
				"openshift-node-tuning-operator",
			}}}},
		{"openshift-apiserver", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-apiserver",
				"openshift-apiserver-operator",
			}}}},
		{"openshift-controller-manager", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-controller-manager",
				"openshift-controller-manager-operator",
			}}}},
		{"openshift-samples", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-cluster-samples-operator",
				"openshift-samples-operator",
			}}}},
		{"operator-lifecycle-manager", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-operator-lifecycle-manager",
			}}}},
		{"service-ca", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-service-ca",
				"openshift-service-ca-operator",
			}}}},
		{"storage", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-storage",
				"openshift-cluster-csi-drivers",
				"openshift-cluster-storage-operator",
				"openshift-storage-operator",
			}}}},
		{"vertical-pod-autoscaler", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-vertical-pod-autoscaler",
				"openshift-vertical-pod-autoscaler-operator",
			}}}},
		{"marketplace", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-marketplace",
				"openshift-marketplace-operator",
			}}}},
	}

	workloadMatchers = []componentMatcher{
		{"openshift-compliance", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-compliance",
			}}}},
		{"openshift-file-integrity", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-file-integrity",
			}}}},
		{"openshift-logging", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-logging",
			}}}},
		{"openshift-user-workload-monitoring", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-user-workload-monitoring",
			}}}},
		{"openshift-gitops", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-gitops",
				"openshift-gitops-operator",
			}}}},
		{"openshift-operators", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-operators",
			}}}},
		{"kubevirt", []LabelsMatcher{
			labelMatcher{"kubernetes_operator_part_of", stringMatcher{
				"kubevirt",
			}},
			labelMatcher{"namespace", stringMatcher{
				"openshift-cnv",
			}},
		}},
		{"openshift-local-storage", []LabelsMatcher{
			labelMatcher{"namespace", stringMatcher{
				"openshift-local-storage",
			}}}},
		{"quay", []LabelsMatcher{
			labelMatcher{"container", stringMatcher{
				"quay-app",
				"quay-mirror",
				"quay-app-upgrade",
			}}}},
		{"Argo", []LabelsMatcher{
			labelMatcher{"alertname", regexpMatcher{
				regexp.MustCompile("^Argo"),
			}}}},
	}
)
