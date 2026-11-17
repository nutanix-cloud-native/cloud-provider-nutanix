//go:build e2e

package e2e

import (
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/cluster-api/test/e2e"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	metalLBNativeManifestURL = "https://raw.githubusercontent.com/metallb/metallb/v0.15.2/config/manifests/metallb-native.yaml"
	metalLBNamespace         = "metallb-system"
	lbTestNamespace          = "lb-e2e"
	lbTestAppLabel           = "lb-e2e"
	noneStaticPoolName       = "e2e-none-static"
	ccmNamespace             = "kube-system"
	ccmDeploymentName        = "nutanix-cloud-controller-manager"
	ccmAppLabel              = "k8s-app=nutanix-cloud-controller-manager"

	// Service LB cleanup can need Prism unreserve + CCM requeue; keep this above
	// the old 5m budget that timed out while the cleanup finalizer was held.
	lbCleanupTimeout = 15 * time.Minute

	loadBalancerIPAnnotation            = "ccm.nutanix.com/load-balancer-ip"
	loadBalancerReservationIDAnnotation = "ccm.nutanix.com/load-balancer-reservation-id"
	loadBalancerSubnetAnnotation        = "ccm.nutanix.com/load-balancer-subnet"
	metalLBRequestedIPsAnnotation       = "metallb.universe.tf/loadBalancerIPs"
	loadBalancerPoolTypeLabel           = "pool-type"
	loadBalancerPoolTypeAutoIPAM        = "auto-ipam-metallb"
	loadBalancerPoolTypeStatic          = "static"
)

var (
	//go:embed data/metallb/l2-autoipam.yaml
	l2AutoIPAMYAML []byte
	//go:embed data/metallb/l2-none.yaml
	l2NoneYAML []byte

	ipAddressPoolGVR = schema.GroupVersionResource{
		Group:    "metallb.io",
		Version:  "v1beta1",
		Resource: "ipaddresspools",
	}
)

// Run alone with LABEL_FILTERS=autoipam. Provisions a CAPX cluster with AutoIPAMMetalLB,
// installs MetalLB, and exercises a LoadBalancer Service through create, connectivity, and delete.
var _ = Describe("LoadBalancer AutoIPAMMetalLB lifecycle", Label("autoipam"), func() {
	subnetName := strings.TrimSpace(os.Getenv("NUTANIX_SUBNET_NAME"))
	if subnetName == "" {
		It("is skipped when NUTANIX_SUBNET_NAME is not configured", func() {
			Skip("Skipping AutoIPAM e2e; set NUTANIX_SUBNET_NAME to run this scenario")
		})
		return
	}

	subnetExtID, err := resolveSubnetExtID(ctx, subnetName)
	if err != nil {
		It("fails fast when the subnet extID cannot be resolved from Prism Central", func() {
			Fail(err.Error())
		})
		return
	}

	e2e.QuickStartSpec(ctx, func() e2e.QuickStartSpecInput {
		return e2e.QuickStartSpecInput{
			E2EConfig:             e2eConfig,
			Flavor:                stringPtr("default-project"),
			ClusterctlConfigPath:  clusterctlConfigPath,
			BootstrapClusterProxy: bootstrapClusterProxy,
			ArtifactFolder:        artifactFolder,
			SkipCleanup:           skipCleanup,
			ClusterctlVariables: map[string]string{
				"NUTANIX_PC_PROJECT":            "",
				"NUTANIX_LOADBALANCER_STRATEGY": "AutoIPAMMetalLB",
				"NUTANIX_SUBNET_EXTID":          subnetExtID,
			},
			PostMachinesProvisioned: func(proxy framework.ClusterProxy, namespace, clusterName string) {
				workload := proxy.GetWorkloadCluster(ctx, namespace, clusterName)
				installMetalLB(workload)
				applyYAML(workload, l2AutoIPAMYAML, "apply AutoIPAM L2Advertisement")

				svc := runLoadBalancerLifecycle(workload, func(service *corev1.Service, ingressIP string) {
					assertAutoIPAMService(workload, service, ingressIP, subnetExtID)
				})

				By("Deleting the LoadBalancer Service and waiting for AutoIPAM release")
				serviceUID := string(svc.UID)
				DeferCleanup(func() {
					_ = unreserveIPsByContext(ctx, subnetExtID, uuid.NewString(), serviceUID)
				})
				Expect(workload.GetClient().Delete(ctx, svc)).To(Succeed())
				waitForLoadBalancerServiceDeleted(workload, client.ObjectKeyFromObject(svc))

				Eventually(func() bool {
					_, err := workloadDynamic(workload).Resource(ipAddressPoolGVR).Namespace(metalLBNamespace).Get(ctx, serviceUID, metav1.GetOptions{})
					return apierrors.IsNotFound(err)
				}, lbCleanupTimeout, 5*time.Second).Should(BeTrue(), "AutoIPAM IPAddressPool should be deleted")

				Eventually(func() []string {
					ips, err := reservedIPsByContext(ctx, subnetExtID, serviceUID)
					Expect(err).NotTo(HaveOccurred())
					return ips
				}, lbCleanupTimeout, 5*time.Second).Should(BeEmpty(), "Prism should have no reservation for Service UID %s", serviceUID)
			},
		}
	})
})

// Run alone with LABEL_FILTERS=lb-none. Provisions a CAPX cluster with strategy None, installs
// MetalLB with a static Prism-backed pool, and checks CCM does not own the LoadBalancer address.
var _ = Describe("LoadBalancer None lifecycle", Label("lb-none"), func() {
	subnetName := strings.TrimSpace(os.Getenv("NUTANIX_SUBNET_NAME"))
	if subnetName == "" {
		It("is skipped when NUTANIX_SUBNET_NAME is not configured", func() {
			Skip("Skipping None LB e2e; set NUTANIX_SUBNET_NAME to run this scenario")
		})
		return
	}

	subnetExtID, err := resolveSubnetExtID(ctx, subnetName)
	if err != nil {
		It("fails fast when the subnet extID cannot be resolved from Prism Central", func() {
			Fail(err.Error())
		})
		return
	}

	e2e.QuickStartSpec(ctx, func() e2e.QuickStartSpecInput {
		return e2e.QuickStartSpecInput{
			E2EConfig:             e2eConfig,
			Flavor:                stringPtr("default-project"),
			ClusterctlConfigPath:  clusterctlConfigPath,
			BootstrapClusterProxy: bootstrapClusterProxy,
			ArtifactFolder:        artifactFolder,
			SkipCleanup:           skipCleanup,
			ClusterctlVariables: map[string]string{
				"NUTANIX_PC_PROJECT":            "",
				"NUTANIX_LOADBALANCER_STRATEGY": "None",
				"NUTANIX_SUBNET_EXTID":          "",
			},
			PostMachinesProvisioned: func(proxy framework.ClusterProxy, namespace, clusterName string) {
				workload := proxy.GetWorkloadCluster(ctx, namespace, clusterName)

				// Unique per run so parallel CI jobs / leftover Prism reservations cannot collide.
				poolClientContext := "e2e-none-pool-" + uuid.NewString()

				By("Reserving a static Prism pool for MetalLB strategy None")
				reserveRequestID := uuid.NewString()
				staticIPs, err := reserveIPs(ctx, subnetExtID, reserveRequestID, poolClientContext, 2)
				Expect(err).NotTo(HaveOccurred())
				Expect(staticIPs).To(HaveLen(2))
				DeferCleanup(func() {
					Expect(unreserveIPsByContext(ctx, subnetExtID, uuid.NewString(), poolClientContext)).To(Succeed())
				})

				installMetalLB(workload)
				applyYAML(workload, l2NoneYAML, "apply static L2Advertisement")
				createStaticIPAddressPool(workload, staticIPs)

				svc := runLoadBalancerLifecycle(workload, func(service *corev1.Service, ingressIP string) {
					Expect(staticIPs).To(ContainElement(ingressIP), "ingress IP should come from the static Prism pool")
					assertNoneService(workload, service)
				})

				By("Deleting the LoadBalancer Service and asserting CCM did not own the reservation")
				serviceUID := string(svc.UID)
				Expect(workload.GetClient().Delete(ctx, svc)).To(Succeed())
				waitForLoadBalancerServiceDeleted(workload, client.ObjectKeyFromObject(svc))

				Consistently(func() error {
					_, err := workloadDynamic(workload).Resource(ipAddressPoolGVR).Namespace(metalLBNamespace).Get(ctx, noneStaticPoolName, metav1.GetOptions{})
					return err
				}, 30*time.Second, 5*time.Second).Should(Succeed(), "static IPAddressPool should remain")

				_, err = workloadDynamic(workload).Resource(ipAddressPoolGVR).Namespace(metalLBNamespace).Get(ctx, serviceUID, metav1.GetOptions{})
				Expect(apierrors.IsNotFound(err)).To(BeTrue(), "CCM must not create a per-Service pool under strategy None")

				ips, err := reservedIPsByContext(ctx, subnetExtID, poolClientContext)
				Expect(err).NotTo(HaveOccurred())
				Expect(ips).To(ConsistOf(staticIPs), "static pool reservations must remain after Service delete")
			},
		}
	})
})

func installMetalLB(workload framework.ClusterProxy) {
	By("Installing MetalLB v0.15.2")
	resp, err := http.Get(metalLBNativeManifestURL)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusOK), "fetch MetalLB manifest")
	manifest, err := io.ReadAll(resp.Body)
	Expect(err).NotTo(HaveOccurred())
	applyYAML(workload, manifest, "apply MetalLB native manifest")

	By("Creating MetalLB memberlist secret")
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "memberlist",
			Namespace: metalLBNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"secretkey": []byte(uuid.NewString() + uuid.NewString()),
		},
	}
	err = workload.GetClient().Create(ctx, secret)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}

	By("Waiting for MetalLB controller and speaker to become Ready")
	Eventually(func(g Gomega) {
		deploy, err := workload.GetClientSet().AppsV1().Deployments(metalLBNamespace).Get(ctx, "controller", metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(deploy.Status.ReadyReplicas).To(BeNumerically(">=", 1))

		ds, err := workload.GetClientSet().AppsV1().DaemonSets(metalLBNamespace).Get(ctx, "speaker", metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(ds.Status.NumberReady).To(BeNumerically(">=", 1))
		g.Expect(ds.Status.NumberReady).To(Equal(ds.Status.DesiredNumberScheduled))
	}, 10*time.Minute, 10*time.Second).Should(Succeed())
}

func applyYAML(workload framework.ClusterProxy, resources []byte, description string) {
	Expect(workload.CreateOrUpdate(ctx, resources, framework.WithPolling(5*time.Minute, 5*time.Second))).To(Succeed(), description)
}

func createStaticIPAddressPool(workload framework.ClusterProxy, ips []string) {
	addresses := make([]any, 0, len(ips))
	for _, ip := range ips {
		addresses = append(addresses, ip+"/32")
	}
	pool := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "metallb.io/v1beta1",
			"kind":       "IPAddressPool",
			"metadata": map[string]any{
				"name":      noneStaticPoolName,
				"namespace": metalLBNamespace,
				"labels": map[string]any{
					loadBalancerPoolTypeLabel: loadBalancerPoolTypeStatic,
				},
			},
			"spec": map[string]any{
				"addresses":  addresses,
				"autoAssign": true,
			},
		},
	}
	_, err := workloadDynamic(workload).Resource(ipAddressPoolGVR).Namespace(metalLBNamespace).Create(ctx, pool, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "create static IPAddressPool")
}

func runLoadBalancerLifecycle(workload framework.ClusterProxy, assertAssigned func(*corev1.Service, string)) *corev1.Service {
	By("Creating agnhost Deployment and LoadBalancer Service")
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: lbTestNamespace}}
	err := workload.GetClient().Create(ctx, ns)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agnhost",
			Namespace: lbTestNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": lbTestAppLabel}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": lbTestAppLabel}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "agnhost",
						Image: "registry.k8s.io/e2e-test-images/agnhost:2.47",
						Args:  []string{"netexec", "--http-port=8080"},
						Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
					}},
				},
			},
		},
	}
	Expect(workload.GetClient().Create(ctx, deploy)).To(Succeed())

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agnhost",
			Namespace: lbTestNamespace,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{"app": lbTestAppLabel},
			Ports: []corev1.ServicePort{{
				Port:       80,
				TargetPort: intstr.FromInt32(8080),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
	Expect(workload.GetClient().Create(ctx, svc)).To(Succeed())

	By("Waiting for LoadBalancer ingress IP")
	var ingressIP string
	Eventually(func(g Gomega) {
		current := &corev1.Service{}
		g.Expect(workload.GetClient().Get(ctx, client.ObjectKeyFromObject(svc), current)).To(Succeed())
		g.Expect(current.Status.LoadBalancer.Ingress).NotTo(BeEmpty())
		ingressIP = strings.TrimSpace(current.Status.LoadBalancer.Ingress[0].IP)
		g.Expect(ingressIP).NotTo(BeEmpty())
		*svc = *current
	}, 10*time.Minute, 5*time.Second).Should(Succeed())

	assertAssigned(svc, ingressIP)
	assertConnectivity(workload, ingressIP)
	return svc
}

func assertAutoIPAMService(workload framework.ClusterProxy, service *corev1.Service, ingressIP, subnetExtID string) {
	Expect(serviceAnnotation(service, loadBalancerIPAnnotation)).To(Equal(ingressIP))
	Expect(serviceAnnotation(service, loadBalancerReservationIDAnnotation)).NotTo(BeEmpty())
	Expect(serviceAnnotation(service, loadBalancerSubnetAnnotation)).To(Equal(subnetExtID))
	Expect(serviceAnnotation(service, metalLBRequestedIPsAnnotation)).To(Equal(ingressIP))

	pool, err := workloadDynamic(workload).Resource(ipAddressPoolGVR).Namespace(metalLBNamespace).Get(ctx, string(service.UID), metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	addresses, found, err := unstructured.NestedStringSlice(pool.Object, "spec", "addresses")
	Expect(err).NotTo(HaveOccurred())
	Expect(found).To(BeTrue())
	Expect(addresses).To(Equal([]string{ingressIP + "/32"}))
	autoAssign, found, err := unstructured.NestedBool(pool.Object, "spec", "autoAssign")
	Expect(err).NotTo(HaveOccurred())
	Expect(found).To(BeTrue())
	Expect(autoAssign).To(BeFalse())
	Expect(pool.GetLabels()[loadBalancerPoolTypeLabel]).To(Equal(loadBalancerPoolTypeAutoIPAM))
}

func assertNoneService(workload framework.ClusterProxy, service *corev1.Service) {
	Expect(serviceAnnotation(service, loadBalancerIPAnnotation)).To(BeEmpty())
	Expect(serviceAnnotation(service, loadBalancerReservationIDAnnotation)).To(BeEmpty())
	Expect(serviceAnnotation(service, loadBalancerSubnetAnnotation)).To(BeEmpty())

	_, err := workloadDynamic(workload).Resource(ipAddressPoolGVR).Namespace(metalLBNamespace).Get(ctx, string(service.UID), metav1.GetOptions{})
	Expect(apierrors.IsNotFound(err)).To(BeTrue(), "strategy None must not create a per-Service IPAddressPool")
}

func serviceAnnotation(service *corev1.Service, key string) string {
	if service == nil || service.Annotations == nil {
		return ""
	}
	return service.Annotations[key]
}

func waitForLoadBalancerServiceDeleted(workload framework.ClusterProxy, key client.ObjectKey) {
	Eventually(func() error {
		err := workload.GetClient().Get(ctx, key, &corev1.Service{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("Service %s/%s still present (cleanup finalizer likely held)", key.Namespace, key.Name)
	}, lbCleanupTimeout, 10*time.Second).Should(Succeed(), func() string {
		return dumpLoadBalancerCleanupDiagnostics(workload, key)
	})
}

func dumpLoadBalancerCleanupDiagnostics(workload framework.ClusterProxy, key client.ObjectKey) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Service %s/%s still present after cleanup timeout\n", key.Namespace, key.Name)

	svc := &corev1.Service{}
	if err := workload.GetClient().Get(ctx, key, svc); err != nil {
		fmt.Fprintf(&b, "get Service: %v\n", err)
	} else {
		fmt.Fprintf(&b, "Service finalizers=%v annotations=%v deletionTimestamp=%v\n",
			svc.Finalizers, svc.Annotations, svc.DeletionTimestamp)
	}

	cs := workload.GetClientSet()
	pods, err := cs.CoreV1().Pods(ccmNamespace).List(ctx, metav1.ListOptions{LabelSelector: ccmAppLabel})
	if err != nil {
		fmt.Fprintf(&b, "list CCM pods: %v\n", err)
		return b.String()
	}
	for _, pod := range pods.Items {
		logs, logErr := cs.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
			Container: ccmDeploymentName,
			TailLines: ptr.To[int64](200),
		}).DoRaw(ctx)
		if logErr != nil {
			fmt.Fprintf(&b, "CCM logs %s/%s: %v\n", pod.Namespace, pod.Name, logErr)
			continue
		}
		fmt.Fprintf(&b, "--- CCM logs %s/%s ---\n%s\n", pod.Namespace, pod.Name, string(logs))
	}
	return b.String()
}

func assertConnectivity(workload framework.ClusterProxy, ingressIP string) {
	Byf("Checking in-cluster connectivity to http://%s", ingressIP)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lb-wget",
			Namespace: lbTestNamespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr.To[int32](4),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{{
						Name:    "wget",
						Image:   "registry.k8s.io/e2e-test-images/agnhost:2.47",
						Command: []string{"/bin/sh", "-c"},
						Args:    []string{fmt.Sprintf("wget -qO- --timeout=5 http://%s:80 || wget -qO- --timeout=5 http://%s", ingressIP, ingressIP)},
					}},
				},
			},
		},
	}
	Expect(workload.GetClient().Create(ctx, job)).To(Succeed())

	Eventually(func(g Gomega) {
		current := &batchv1.Job{}
		g.Expect(workload.GetClient().Get(ctx, client.ObjectKeyFromObject(job), current)).To(Succeed())
		g.Expect(current.Status.Succeeded).To(BeNumerically(">=", 1))
	}, 5*time.Minute, 5*time.Second).Should(Succeed())
}

func workloadDynamic(workload framework.ClusterProxy) dynamic.Interface {
	dyn, err := dynamic.NewForConfig(workload.GetRESTConfig())
	Expect(err).NotTo(HaveOccurred())
	return dyn
}
