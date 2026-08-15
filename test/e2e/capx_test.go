//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/test/e2e"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	customPEUUIDLabel            = "nutanix.com/prism-element-uuid"
	customProjectUUIDLabel       = "nutanix.com/project-uuid"
	customResourceGroupUUIDLabel = "nutanix.com/resource-group-uuid"
	zeroUUID                     = "00000000-0000-0000-0000-000000000000"

	// pcProjectEnv names the Prism Central project used by both the non-default-project and
	// project-scoped-user scenarios; it's the same target project in both cases, just accessed
	// with different credentials.
	pcProjectEnv = "NUTANIX_PROJECT_SCOPED_PC_PROJECT"
)

var _ = Describe("CCM on a CAPX Cluster (default project, non-project-scoped creds)", Label("capx", "default-project"), func() {
	e2e.QuickStartSpec(ctx, func() e2e.QuickStartSpecInput {
		return e2e.QuickStartSpecInput{
			E2EConfig:             e2eConfig,
			Flavor:                stringPtr("default-project"),
			ClusterctlConfigPath:  clusterctlConfigPath,
			BootstrapClusterProxy: bootstrapClusterProxy,
			ArtifactFolder:        artifactFolder,
			SkipCleanup:           skipCleanup,
			ClusterctlVariables: map[string]string{
				"NUTANIX_PC_PROJECT": "",
			},
		}
	})
})

var _ = Describe("CCM on a CAPX cluster provisioned with a project-scoped user", Label("capx", "project-scoped-user"), func() {
	const (
		projectScopedUserEnv      = "NUTANIX_PROJECT_SCOPED_USER"
		projectScopedPasswordEnv  = "NUTANIX_PROJECT_SCOPED_PASSWORD"
		projectScopedImageNameEnv = "NUTANIX_PROJECT_SCOPED_MACHINE_TEMPLATE_IMAGE_NAME"
	)
	projectScopedUser := strings.TrimSpace(os.Getenv(projectScopedUserEnv))
	projectScopedPassword := strings.TrimSpace(os.Getenv(projectScopedPasswordEnv))
	pcProject := strings.TrimSpace(os.Getenv(pcProjectEnv))

	if projectScopedUser == "" || projectScopedPassword == "" || pcProject == "" {
		It("is skipped when project-scoped env is not configured", func() {
			Skip(fmt.Sprintf(
				"Skipping project-scoped E2E; set %s, %s and %s to run this scenario",
				projectScopedUserEnv,
				projectScopedPasswordEnv,
				pcProjectEnv,
			))
		})
		return
	}

	expectedProjectUUID, expectedResourceGroupUUID, err := resolveProjectAndResourceGroupUUIDs(ctx, pcProject)
	if err != nil {
		It("fails fast when the project/resource-group UUIDs cannot be resolved from Prism Central", func() {
			Fail(err.Error())
		})
		return
	}

	e2e.QuickStartSpec(ctx, func() e2e.QuickStartSpecInput {
		clusterctlVariables := map[string]string{
			"NUTANIX_PROJECT_SCOPED_USER":     projectScopedUser,
			"NUTANIX_PROJECT_SCOPED_PASSWORD": projectScopedPassword,
			"NUTANIX_PC_PROJECT":              pcProject,
		}
		// e2eConfig is only populated once SynchronizedBeforeSuite has run, so this lookup must
		// happen here (evaluated lazily at spec run time), not in the outer Describe body.
		if projectScopedImageName := strings.TrimSpace(e2eConfig.GetVariableOrEmpty(projectScopedImageNameEnv)); projectScopedImageName != "" {
			clusterctlVariables["NUTANIX_MACHINE_TEMPLATE_IMAGE_NAME"] = projectScopedImageName
		}

		return e2e.QuickStartSpecInput{
			E2EConfig:             e2eConfig,
			Flavor:                stringPtr("project-scoped-user"),
			ClusterctlConfigPath:  clusterctlConfigPath,
			BootstrapClusterProxy: bootstrapClusterProxy,
			ArtifactFolder:        artifactFolder,
			SkipCleanup:           skipCleanup,
			ClusterctlVariables:   clusterctlVariables,
			PostMachinesProvisioned: func(proxy framework.ClusterProxy, namespace, clusterName string) {
				By("Checking machine progression signals for project-scoped run")
				machines := framework.GetMachinesByCluster(ctx, framework.GetMachinesByClusterInput{
					Lister:      proxy.GetClient(),
					ClusterName: clusterName,
					Namespace:   namespace,
				})
				Expect(machines).ToNot(BeEmpty(), "expected Machines for cluster %q", clusterName)
				for i := range machines {
					machine := machines[i]
					Expect(machine.Status.NodeRef.IsDefined()).To(BeTrue(), "expected NodeRef for Machine %q", machine.Name)
					Expect(machine.Status.Phase).ToNot(Equal(string(clusterv1.MachinePhaseProvisioning)),
						"Machine %q still in Provisioning; project-scoped API flow may be stuck", machine.Name)
				}

				By("Checking workload node metadata convergence (ProviderID and InternalIP)")
				workloadClusterProxy := proxy.GetWorkloadCluster(ctx, namespace, clusterName)
				nodeList := &corev1.NodeList{}
				Expect(workloadClusterProxy.GetClient().List(ctx, nodeList)).To(Succeed())
				Expect(nodeList.Items).ToNot(BeEmpty(), "expected workload Nodes for cluster %q", clusterName)
				for i := range nodeList.Items {
					node := nodeList.Items[i]
					Expect(strings.TrimSpace(node.Spec.ProviderID)).ToNot(BeEmpty(),
						"expected ProviderID on Node %q", node.Name)
					Expect(hasInternalIP(node)).To(BeTrue(),
						"expected InternalIP on Node %q", node.Name)
					assertScopedProjectAndResourceGroupLabels(node, expectedProjectUUID, expectedResourceGroupUUID)
					Expect(hasNodeLabel(node, customPEUUIDLabel)).To(BeTrue(),
						"expected PE UUID label %q on Node %q", customPEUUIDLabel, node.Name)
				}

				By("Checking scoped credential and pc-project wiring in rendered resources")
				workloadCredSecret := &corev1.Secret{}
				Expect(proxy.GetClient().Get(
					ctx,
					client.ObjectKey{Namespace: namespace, Name: clusterName},
					workloadCredSecret,
				)).To(Succeed())
				Expect(string(workloadCredSecret.Data["credentials"])).To(ContainSubstring(projectScopedUser),
					"expected workload credentials secret to use project-scoped user")

				ccmConfigMap := &corev1.ConfigMap{}
				Expect(proxy.GetClient().Get(
					ctx,
					client.ObjectKey{Namespace: namespace, Name: "nutanix-ccm"},
					ccmConfigMap,
				)).To(Succeed())
				Expect(ccmConfigMap.Data["nutanix-ccm.yaml"]).To(ContainSubstring(`"pc-project": "`+pcProject+`"`),
					"expected nutanix CCM config to include configured pc-project input")

				ccmPods := &corev1.PodList{}
				Expect(workloadClusterProxy.GetClient().List(
					ctx,
					ccmPods,
					client.InNamespace("kube-system"),
					client.MatchingLabels{"k8s-app": "nutanix-cloud-controller-manager"},
				)).To(Succeed())
				Expect(ccmPods.Items).ToNot(BeEmpty(),
					"expected nutanix CCM pod in kube-system for cluster %q", clusterName)

				assertCCMReconciliationObservability(workloadClusterProxy, ccmPods, []string{
					"forbidden",
					"clusters/list",
					"/api/nutanix/v3/clusters",
					"failed to refresh project scope",
					"error occurred while updating labels on node",
				})
			},
		}
	})
})

var _ = Describe("CCM on a CAPX cluster provisioned with a non-project-scoped user, routed to a non-default project", Label("capx", "non-default-project"), func() {
	const nonProjectScopedImageNameEnv = "NUTANIX_NON_PROJECT_SCOPED_MACHINE_TEMPLATE_IMAGE_NAME"
	expectedPCProject := strings.TrimSpace(os.Getenv(pcProjectEnv))

	if expectedPCProject == "" {
		It("is skipped when project-routing env is not configured", func() {
			Skip(fmt.Sprintf(
				"Skipping non-project-scoped/project-routing E2E; set %s to run this scenario",
				pcProjectEnv,
			))
		})
		return
	}

	expectedProjectUUID, expectedResourceGroupUUID, err := resolveProjectAndResourceGroupUUIDs(ctx, expectedPCProject)
	if err != nil {
		It("fails fast when the project/resource-group UUIDs cannot be resolved from Prism Central", func() {
			Fail(err.Error())
		})
		return
	}

	e2e.QuickStartSpec(ctx, func() e2e.QuickStartSpecInput {
		clusterctlVariables := map[string]string{
			"NUTANIX_PC_PROJECT": expectedPCProject,
		}
		// e2eConfig is only populated once SynchronizedBeforeSuite has run, so this lookup must
		// happen here (evaluated lazily at spec run time), not in the outer Describe body.
		if nonProjectScopedImageName := strings.TrimSpace(e2eConfig.GetVariableOrEmpty(nonProjectScopedImageNameEnv)); nonProjectScopedImageName != "" {
			clusterctlVariables["NUTANIX_MACHINE_TEMPLATE_IMAGE_NAME"] = nonProjectScopedImageName
		}

		return e2e.QuickStartSpecInput{
			E2EConfig:             e2eConfig,
			Flavor:                stringPtr("non-default-project"),
			ClusterctlConfigPath:  clusterctlConfigPath,
			BootstrapClusterProxy: bootstrapClusterProxy,
			ArtifactFolder:        artifactFolder,
			SkipCleanup:           skipCleanup,
			ClusterctlVariables:   clusterctlVariables,
			PostMachinesProvisioned: func(proxy framework.ClusterProxy, namespace, clusterName string) {
				By("Checking machine progression signals for non-project-scoped run")
				machines := framework.GetMachinesByCluster(ctx, framework.GetMachinesByClusterInput{
					Lister:      proxy.GetClient(),
					ClusterName: clusterName,
					Namespace:   namespace,
				})
				Expect(machines).ToNot(BeEmpty(), "expected Machines for cluster %q", clusterName)
				for i := range machines {
					machine := machines[i]
					Expect(machine.Status.NodeRef.IsDefined()).To(BeTrue(), "expected NodeRef for Machine %q", machine.Name)
					Expect(machine.Status.Phase).ToNot(Equal(string(clusterv1.MachinePhaseProvisioning)),
						"Machine %q still in Provisioning; non-project-scoped flow may be stuck", machine.Name)
				}

				By("Checking workload node metadata convergence and VM project-label behavior")
				workloadClusterProxy := proxy.GetWorkloadCluster(ctx, namespace, clusterName)
				nodeList := &corev1.NodeList{}
				Expect(workloadClusterProxy.GetClient().List(ctx, nodeList)).To(Succeed())
				Expect(nodeList.Items).ToNot(BeEmpty(), "expected workload Nodes for cluster %q", clusterName)
				for i := range nodeList.Items {
					node := nodeList.Items[i]
					Expect(strings.TrimSpace(node.Spec.ProviderID)).ToNot(BeEmpty(),
						"expected ProviderID on Node %q", node.Name)
					Expect(hasInternalIP(node)).To(BeTrue(),
						"expected InternalIP on Node %q", node.Name)
					assertScopedProjectAndResourceGroupLabels(node, expectedProjectUUID, expectedResourceGroupUUID)
					Expect(hasNodeLabel(node, customPEUUIDLabel)).To(BeTrue(),
						"expected PE UUID label %q on Node %q", customPEUUIDLabel, node.Name)
				}

				By("Checking non-project-scoped credentials with project routing in rendered resources")
				ccmConfigMap := &corev1.ConfigMap{}
				Expect(proxy.GetClient().Get(
					ctx,
					client.ObjectKey{Namespace: namespace, Name: "nutanix-ccm"},
					ccmConfigMap,
				)).To(Succeed())
				Expect(ccmConfigMap.Data["nutanix-ccm.yaml"]).To(ContainSubstring(`"pc-project": "`+expectedPCProject+`"`),
					"expected nutanix CCM config to include configured pc-project input")

				ccmPods := &corev1.PodList{}
				Expect(workloadClusterProxy.GetClient().List(
					ctx,
					ccmPods,
					client.InNamespace("kube-system"),
					client.MatchingLabels{"k8s-app": "nutanix-cloud-controller-manager"},
				)).To(Succeed())
				Expect(ccmPods.Items).ToNot(BeEmpty(),
					"expected nutanix CCM pod in kube-system for cluster %q", clusterName)

				assertCCMReconciliationObservability(workloadClusterProxy, ccmPods, []string{
					"forbidden",
					"clusters/list",
					"/api/nutanix/v3/clusters",
					"failed to refresh project scope",
					"error occurred while updating labels on node",
				})
			},
		}
	})
})

func hasInternalIP(node corev1.Node) bool {
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP && strings.TrimSpace(addr.Address) != "" {
			return true
		}
	}
	return false
}

func hasNodeLabel(node corev1.Node, key string) bool {
	val, ok := node.Labels[key]
	return ok && strings.TrimSpace(val) != ""
}

func assertScopedProjectAndResourceGroupLabels(node corev1.Node, expectedProjectUUID, expectedResourceGroupUUID string) {
	projectUUID := strings.TrimSpace(node.Labels[customProjectUUIDLabel])
	resourceGroupUUID := strings.TrimSpace(node.Labels[customResourceGroupUUIDLabel])

	Expect(projectUUID).ToNot(Equal(zeroUUID),
		"expected project label %q on Node %q to be project-routed UUID (not zero UUID)", customProjectUUIDLabel, node.Name)
	Expect(projectUUID).To(Equal(expectedProjectUUID),
		"expected project label %q on Node %q to match configured expected UUID", customProjectUUIDLabel, node.Name)
	Expect(resourceGroupUUID).ToNot(Equal(zeroUUID),
		"expected resource-group label %q on Node %q to be routed resource-group UUID (not zero UUID)", customResourceGroupUUIDLabel, node.Name)
	Expect(resourceGroupUUID).To(Equal(expectedResourceGroupUUID),
		"expected resource-group label %q on Node %q to match configured expected UUID", customResourceGroupUUIDLabel, node.Name)
}

func stringPtr(v string) *string {
	return &v
}

func isPodReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func assertCCMReconciliationObservability(workloadClusterProxy framework.ClusterProxy, ccmPods *corev1.PodList, disallowedLogHints []string) {
	for _, pod := range ccmPods.Items {
		Expect(pod.Status.Phase).To(Equal(corev1.PodRunning), "expected CCM pod %q to be Running", pod.Name)
		Expect(isPodReady(pod)).To(BeTrue(), "expected CCM pod %q to be Ready", pod.Name)

		for _, status := range pod.Status.ContainerStatuses {
			if status.Name != "nutanix-cloud-controller-manager" {
				continue
			}
			Expect(status.RestartCount).To(BeNumerically("<", 3),
				"CCM container in pod %q restarted too often; may indicate reconciliation/scope-refresh instability", pod.Name)
		}

		logs, err := workloadClusterProxy.GetClientSet().CoreV1().Pods("kube-system").GetLogs(
			pod.Name,
			&corev1.PodLogOptions{Container: "nutanix-cloud-controller-manager"},
		).DoRaw(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed reading logs for CCM pod %q", pod.Name)
		logText := strings.ToLower(string(logs))
		for _, hint := range disallowedLogHints {
			Expect(logText).ToNot(ContainSubstring(hint),
				"detected suspicious reconciliation/scope-refresh symptom %q in CCM pod %q logs", hint, pod.Name)
		}
	}
}
