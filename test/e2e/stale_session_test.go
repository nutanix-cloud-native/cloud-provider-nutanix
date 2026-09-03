//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	capi_e2e "sigs.k8s.io/cluster-api/test/e2e"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ccmDeploymentName       = "nutanix-cloud-controller-manager"
	ccmDeploymentNamespace  = "kube-system"
	envStaleSessionWait     = "STALE_SESSION_WAIT"
	envConcurrentNodeSyncs  = "CONCURRENT_NODE_SYNCS"
	defaultStaleSessionWait = 20 * time.Minute
	defaultConcurrentSyncs  = 10
	cloudProviderTaintKey   = "node.cloudprovider.kubernetes.io/uninitialized"
)

// Labeled only "stale-session" (not "capx") so CI LABEL_FILTERS=capx never selects it.
// Opt-in: LABEL_FILTERS=stale-session GINKGO_TIMEOUT=90m make test-e2e
var _ = Describe("CCM stale PC session recovery", Label("stale-session"), func() {
	var (
		specName         = "stale-session"
		namespace        *corev1.Namespace
		cancelWatches    context.CancelFunc
		clusterResources *clusterctl.ApplyClusterTemplateAndWaitResult
	)

	BeforeEach(func() {
		Expect(e2eConfig).ToNot(BeNil())
		Expect(clusterctlConfigPath).To(BeAnExistingFile())
		Expect(bootstrapClusterProxy).ToNot(BeNil())
		Expect(os.MkdirAll(artifactFolder, 0o750)).To(Succeed())
		Expect(e2eConfig.Variables).To(HaveKey(capi_e2e.KubernetesVersion))

		namespace, cancelWatches = framework.SetupSpecNamespace(ctx, specName, bootstrapClusterProxy, artifactFolder, nil)
		clusterResources = new(clusterctl.ApplyClusterTemplateAndWaitResult)
	})

	AfterEach(func() {
		framework.DumpSpecResourcesAndCleanup(
			ctx,
			specName,
			bootstrapClusterProxy,
			clusterctlConfigPath,
			artifactFolder,
			namespace,
			cancelWatches,
			clusterResources.Cluster,
			e2eConfig.GetIntervals,
			skipCleanup,
		)
	})

	It("recovers after real PC session expiry with concurrent node syncs and worker scale-out", func() {
		By("Creating a workload cluster with CCM")
		clusterName := fmt.Sprintf("%s-%s", specName, namespace.Name)
		clusterctl.ApplyClusterTemplateAndWait(ctx, clusterctl.ApplyClusterTemplateAndWaitInput{
			ClusterProxy: bootstrapClusterProxy,
			ConfigCluster: clusterctl.ConfigClusterInput{
				LogFolder:                filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName()),
				ClusterctlConfigPath:     clusterctlConfigPath,
				KubeconfigPath:           bootstrapClusterProxy.GetKubeconfigPath(),
				InfrastructureProvider:   clusterctl.DefaultInfrastructureProvider,
				Flavor:                   clusterctl.DefaultFlavor,
				Namespace:                namespace.Name,
				ClusterName:              clusterName,
				KubernetesVersion:        e2eConfig.MustGetVariable(capi_e2e.KubernetesVersion),
				ControlPlaneMachineCount: ptr.To[int64](1),
				WorkerMachineCount:       ptr.To[int64](1),
			},
			WaitForClusterIntervals:      e2eConfig.GetIntervals(specName, "wait-cluster"),
			WaitForControlPlaneIntervals: e2eConfig.GetIntervals(specName, "wait-control-plane"),
			WaitForMachineDeployments:    e2eConfig.GetIntervals(specName, "wait-worker-nodes"),
		}, clusterResources)

		Expect(clusterResources.MachineDeployments).ToNot(BeEmpty())
		md := clusterResources.MachineDeployments[0]
		initialReplicas := int32(1)
		if md.Spec.Replicas != nil {
			initialReplicas = *md.Spec.Replicas
		}

		workloadProxy := bootstrapClusterProxy.GetWorkloadCluster(ctx, namespace.Name, clusterName)
		Expect(workloadProxy).ToNot(BeNil())

		syncs := concurrentNodeSyncsFromEnv()
		By(fmt.Sprintf("Patching CCM with --concurrent-node-syncs=%d", syncs))
		patchCCMConcurrentNodeSyncs(ctx, workloadProxy.GetClient(), syncs)

		waitFor := staleSessionWaitFromEnv()
		By(fmt.Sprintf("Waiting %s for real PC IAM session expiry (not StripSDKAuthorization)", waitFor))
		time.Sleep(waitFor)

		targetReplicas := initialReplicas + 1
		By(fmt.Sprintf("Scaling MachineDeployment workers from %d to %d after session expiry", initialReplicas, targetReplicas))
		framework.ScaleAndWaitMachineDeployment(ctx, framework.ScaleAndWaitMachineDeploymentInput{
			ClusterProxy:              bootstrapClusterProxy,
			Cluster:                   clusterResources.Cluster,
			MachineDeployment:         md,
			Replicas:                  targetReplicas,
			WaitForMachineDeployments: e2eConfig.GetIntervals(specName, "wait-worker-nodes"),
		})

		By("Asserting new worker nodes are Ready without uninitialized cloud taint")
		Eventually(func(g Gomega) {
			nodes := &corev1.NodeList{}
			g.Expect(workloadProxy.GetClient().List(ctx, nodes)).To(Succeed())
			readyWorkers := 0
			for i := range nodes.Items {
				node := &nodes.Items[i]
				if isControlPlaneNode(node) {
					continue
				}
				g.Expect(nodeIsReady(node)).To(BeTrue(), "worker node %s should be Ready", node.Name)
				g.Expect(nodeHasTaint(node, cloudProviderTaintKey)).To(BeFalse(),
					"worker node %s must not retain %s after CCM recovers", node.Name, cloudProviderTaintKey)
				readyWorkers++
			}
			g.Expect(readyWorkers).To(BeNumerically(">=", int(targetReplicas)))
		}, e2eConfig.GetIntervals(specName, "wait-nodes-ready")...).Should(Succeed())

		By("Asserting CCM Deployment remains Available")
		Eventually(func(g Gomega) {
			deploy := &appsv1.Deployment{}
			g.Expect(workloadProxy.GetClient().Get(ctx, types.NamespacedName{
				Namespace: ccmDeploymentNamespace,
				Name:      ccmDeploymentName,
			}, deploy)).To(Succeed())
			g.Expect(deploy.Status.ReadyReplicas).To(BeNumerically(">=", int32(1)))
		}, "2m", "5s").Should(Succeed())

		By("Best-effort: CCM logs should not show sustained unsupported protocol scheme errors")
		assertNoSustainedUnsupportedSchemeInCCMLogs(ctx, workloadProxy)
	})
})

func staleSessionWaitFromEnv() time.Duration {
	if raw := strings.TrimSpace(os.Getenv(envStaleSessionWait)); raw != "" {
		d, err := time.ParseDuration(raw)
		Expect(err).NotTo(HaveOccurred(), "invalid %s=%q", envStaleSessionWait, raw)
		return d
	}
	return defaultStaleSessionWait
}

func concurrentNodeSyncsFromEnv() int {
	if raw := strings.TrimSpace(os.Getenv(envConcurrentNodeSyncs)); raw != "" {
		n, err := strconv.Atoi(raw)
		Expect(err).NotTo(HaveOccurred(), "invalid %s=%q", envConcurrentNodeSyncs, raw)
		Expect(n).To(BeNumerically(">", 0))
		return n
	}
	return defaultConcurrentSyncs
}

func patchCCMConcurrentNodeSyncs(ctx context.Context, c client.Client, syncs int) {
	deploy := &appsv1.Deployment{}
	Eventually(func(g Gomega) {
		err := c.Get(ctx, types.NamespacedName{
			Namespace: ccmDeploymentNamespace,
			Name:      ccmDeploymentName,
		}, deploy)
		if apierrors.IsNotFound(err) {
			g.Expect(err).NotTo(HaveOccurred(),
				"CCM Deployment %s/%s is missing", ccmDeploymentNamespace, ccmDeploymentName)
			return
		}
		g.Expect(err).NotTo(HaveOccurred(),
			"workload API unreachable while getting CCM Deployment %s/%s", ccmDeploymentNamespace, ccmDeploymentName)
		g.Expect(deploy.Spec.Template.Spec.Containers).NotTo(BeEmpty(),
			"CCM Deployment %s/%s has no containers", ccmDeploymentNamespace, ccmDeploymentName)
	}, e2eConfig.GetIntervals("stale-session", "wait-nodes-ready")...).Should(Succeed())
	args := deploy.Spec.Template.Spec.Containers[0].Args
	flag := fmt.Sprintf("--concurrent-node-syncs=%d", syncs)
	replaced := false
	for i, a := range args {
		if strings.HasPrefix(a, "--concurrent-node-syncs=") {
			args[i] = flag
			replaced = true
			break
		}
	}
	if !replaced {
		args = append(args, flag)
	}
	deploy.Spec.Template.Spec.Containers[0].Args = args
	Expect(c.Update(ctx, deploy)).To(Succeed())

	Eventually(func(g Gomega) {
		d := &appsv1.Deployment{}
		g.Expect(c.Get(ctx, types.NamespacedName{
			Namespace: ccmDeploymentNamespace,
			Name:      ccmDeploymentName,
		}, d)).To(Succeed())
		g.Expect(d.Status.UpdatedReplicas).To(Equal(d.Status.Replicas))
		g.Expect(d.Status.ReadyReplicas).To(Equal(d.Status.Replicas))
		g.Expect(d.Status.UnavailableReplicas).To(BeZero())
	}, "5m", "5s").Should(Succeed())
}

func isControlPlaneNode(node *corev1.Node) bool {
	if _, ok := node.Labels["node-role.kubernetes.io/control-plane"]; ok {
		return true
	}
	_, ok := node.Labels["node-role.kubernetes.io/master"]
	return ok
}

func nodeIsReady(node *corev1.Node) bool {
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func nodeHasTaint(node *corev1.Node, key string) bool {
	for _, t := range node.Spec.Taints {
		if t.Key == key {
			return true
		}
	}
	return false
}

func assertNoSustainedUnsupportedSchemeInCCMLogs(ctx context.Context, workloadProxy framework.ClusterProxy) {
	cs := workloadProxy.GetClientSet()
	pods, err := cs.CoreV1().Pods(ccmDeploymentNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "k8s-app=" + ccmDeploymentName,
	})
	Expect(err).NotTo(HaveOccurred())
	if len(pods.Items) == 0 {
		By("Skipping CCM log assertion: no pods matched CCM label selector")
		return
	}

	podName := pods.Items[0].Name
	req := cs.CoreV1().Pods(ccmDeploymentNamespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: ccmDeploymentName,
		TailLines: ptr.To[int64](200),
	})
	raw, err := req.Do(ctx).Raw()
	if err != nil {
		By(fmt.Sprintf("Skipping CCM log assertion: failed to fetch logs: %v", err))
		return
	}
	schemeErrs := strings.Count(string(raw), "unsupported protocol scheme")
	Expect(schemeErrs).To(BeNumerically("<", 20),
		"CCM logs show sustained unsupported protocol scheme failures (%d occurrences in last 200 lines)", schemeErrs)
}
