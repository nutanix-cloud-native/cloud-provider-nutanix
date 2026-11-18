//go:build integration

/*
Copyright 2026 Nutanix, Inc

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package autoipam

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	convergedV4 "github.com/nutanix-cloud-native/prism-go-client/converged/v4"
	"github.com/nutanix-cloud-native/prism-go-client/environment/credentials"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	cloudprovider "k8s.io/cloud-provider"
	servicecontroller "k8s.io/cloud-provider/controllers/service"
	"k8s.io/component-base/featuregate"
	controllersmetrics "k8s.io/component-base/metrics/prometheus/controllers"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/loadbalancer"
)

func init() {
	// Prevent controller-runtime from complaining about log.SetLogger not being called.
	logf.SetLogger(zap.New(zap.UseDevMode(true)))
}

const (
	ccmNamespace       = "kube-system"
	credSecretName     = "nutanix-creds"
	defaultMetalLBNS   = "metallb-system"
	integrationCluster = "integration"
)

var ipAddressPoolGVR = schema.GroupVersionResource{
	Group:    "metallb.io",
	Version:  "v1beta1",
	Resource: "ipaddresspools",
}

type harness struct {
	t            *testing.T
	ctx          context.Context
	cancel       context.CancelFunc
	env          *envtest.Environment
	cfg          *rest.Config
	kube         kubernetes.Interface
	dyn          dynamic.Interface
	cloud        cloudprovider.Interface
	prismCleanup *convergedV4.Client
	subnetExtID  string
	metalLBNS    string
	endpoint     string
}

func requiredEnv(t *testing.T) (endpoint, subnetName string, ok bool) {
	t.Helper()
	endpoint = strings.TrimSpace(os.Getenv("NUTANIX_ENDPOINT"))
	subnetName = strings.TrimSpace(os.Getenv("NUTANIX_SUBNET_NAME"))
	hasCreds := strings.TrimSpace(os.Getenv("NUTANIX_API_KEY")) != "" ||
		(strings.TrimSpace(firstNonEmpty(os.Getenv("NUTANIX_USER"), os.Getenv("NUTANIX_USERNAME"))) != "" &&
			strings.TrimSpace(os.Getenv("NUTANIX_PASSWORD")) != "")
	if endpoint == "" || subnetName == "" || !hasCreds {
		return "", "", false
	}
	return endpoint, subnetName, true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func startHarness(t *testing.T) *harness {
	t.Helper()
	endpoint, subnetName, ok := requiredEnv(t)
	if !ok {
		t.Skip("set NUTANIX_ENDPOINT, NUTANIX_SUBNET_NAME, and credentials (NUTANIX_API_KEY or NUTANIX_USER/NUTANIX_PASSWORD)")
	}

	metalLBNS := strings.TrimSpace(os.Getenv("NUTANIX_AUTOIPAM_METALLB_NAMESPACE"))
	if metalLBNS == "" {
		metalLBNS = defaultMetalLBNS
	}

	if err := os.Setenv(constants.CCMNamespaceKey, ccmNamespace); err != nil {
		t.Fatalf("set POD_NAMESPACE: %v", err)
	}

	prismCleanup, err := provider.ConvergedClientFromLocalEnv()
	if err != nil {
		t.Fatalf("prism cleanup client: %v", err)
	}
	subnetExtID, err := provider.SubnetExtIDByName(context.Background(), prismCleanup, subnetName)
	if err != nil {
		t.Fatalf("resolve subnet %q: %v", subnetName, err)
	}

	crdPath, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatalf("testdata path: %v", err)
	}
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{crdPath},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}

	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		_ = testEnv.Stop()
		t.Fatalf("kube client: %v", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		_ = testEnv.Stop()
		t.Fatalf("dynamic client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{
		t:            t,
		ctx:          ctx,
		cancel:       cancel,
		env:          testEnv,
		cfg:          cfg,
		kube:         kube,
		dyn:          dyn,
		prismCleanup: prismCleanup,
		subnetExtID:  subnetExtID,
		metalLBNS:    metalLBNS,
		endpoint:     endpoint,
	}
	t.Cleanup(h.stop)

	if err := h.ensureNamespaces(); err != nil {
		t.Fatalf("namespaces: %v", err)
	}
	if err := h.seedReadyNodes(); err != nil {
		t.Fatalf("nodes: %v", err)
	}
	if err := h.seedCredentials(); err != nil {
		t.Fatalf("credentials: %v", err)
	}

	cloudConfig, err := h.cloudConfigJSON()
	if err != nil {
		t.Fatalf("cloud config: %v", err)
	}
	cloud, err := cloudprovider.GetCloudProvider(constants.ProviderName, strings.NewReader(cloudConfig))
	if err != nil {
		t.Fatalf("GetCloudProvider: %v", err)
	}
	if cloud == nil {
		t.Fatal("GetCloudProvider returned nil")
	}
	h.cloud = cloud

	builder := clientbuilder.SimpleControllerClientBuilder{ClientConfig: cfg}
	cloud.Initialize(builder, ctx.Done())

	if err := h.startServiceController(); err != nil {
		t.Fatalf("service controller: %v", err)
	}
	return h
}

func (h *harness) stop() {
	h.cancel()
	if h.env != nil {
		if err := h.env.Stop(); err != nil {
			h.t.Logf("stop envtest: %v", err)
		}
	}
}

func (h *harness) ensureNamespaces() error {
	for _, ns := range []string{ccmNamespace, h.metalLBNS, "default"} {
		_, err := h.kube.CoreV1().Namespaces().Create(h.ctx, &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}

// seedReadyNodes creates Ready worker nodes so the Service controller does not
// emit UnAvailableLoadBalancer during EnsureLoadBalancer.
func (h *harness) seedReadyNodes() error {
	nodes := []struct {
		name string
		ip   string
	}{
		{name: "worker-a", ip: "10.0.0.1"},
		{name: "worker-b", ip: "10.0.0.2"},
	}
	for _, n := range nodes {
		status := v1.NodeStatus{
			Conditions: []v1.NodeCondition{{
				Type:   v1.NodeReady,
				Status: v1.ConditionTrue,
			}},
			Addresses: []v1.NodeAddress{
				{Type: v1.NodeInternalIP, Address: n.ip},
			},
		}
		created, err := h.kube.CoreV1().Nodes().Create(h.ctx, &v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: n.name},
		}, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create node %s: %w", n.name, err)
		}
		if apierrors.IsAlreadyExists(err) {
			created, err = h.kube.CoreV1().Nodes().Get(h.ctx, n.name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("get node %s: %w", n.name, err)
			}
		}
		// Status is often dropped on create; set Ready explicitly.
		created = created.DeepCopy()
		created.Status = status
		if _, err := h.kube.CoreV1().Nodes().UpdateStatus(h.ctx, created, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update node %s status: %w", n.name, err)
		}
	}
	return nil
}

func (h *harness) seedCredentials() error {
	credsJSON, err := credentialsJSONFromEnv()
	if err != nil {
		return err
	}
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      credSecretName,
			Namespace: ccmNamespace,
		},
		Data: map[string][]byte{
			credentials.KeyName: []byte(credsJSON),
		},
	}
	_, err = h.kube.CoreV1().Secrets(ccmNamespace).Create(h.ctx, secret, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func credentialsJSONFromEnv() (string, error) {
	if key := strings.TrimSpace(os.Getenv("NUTANIX_API_KEY")); key != "" {
		return fmt.Sprintf(`[
  {
    "type": "api_key",
    "data": {
      "prismCentral": {
        "apiKey": %q
      }
    }
  }
]`, key), nil
	}
	user := firstNonEmpty(os.Getenv("NUTANIX_USER"), os.Getenv("NUTANIX_USERNAME"))
	pass := os.Getenv("NUTANIX_PASSWORD")
	if strings.TrimSpace(user) == "" || strings.TrimSpace(pass) == "" {
		return "", fmt.Errorf("missing NUTANIX_API_KEY or NUTANIX_USER/NUTANIX_PASSWORD")
	}
	return fmt.Sprintf(`[
  {
    "type": "basic_auth",
    "data": {
      "prismCentral": {
        "username": %q,
        "password": %q
      }
    }
  }
]`, user, pass), nil
}

func (h *harness) cloudConfigJSON() (string, error) {
	port := 9440
	if p := strings.TrimSpace(os.Getenv("NUTANIX_PORT")); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return "", fmt.Errorf("NUTANIX_PORT: %w", err)
		}
		port = n
	}
	insecure := true
	if v := strings.TrimSpace(os.Getenv("NUTANIX_INSECURE")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return "", fmt.Errorf("NUTANIX_INSECURE: %w", err)
		}
		insecure = b
	}
	cfg := map[string]any{
		"prismCentral": map[string]any{
			"address":  h.endpoint,
			"port":     port,
			"insecure": insecure,
			"credentialRef": map[string]any{
				"kind":      "secret",
				"name":      credSecretName,
				"namespace": ccmNamespace,
			},
		},
		"topologyDiscovery": map[string]any{
			"type": "Prism",
		},
		"enableCustomLabeling": false,
		"loadBalancer": map[string]any{
			"defaultStrategy": loadbalancer.StrategyAutoIPAMMetalLB,
			"autoIPAMMetalLB": map[string]any{
				"subnet":           h.subnetExtID,
				"metalLBNamespace": h.metalLBNS,
			},
		},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (h *harness) startServiceController() error {
	factory := informers.NewSharedInformerFactory(h.kube, 0)
	ctrl, err := servicecontroller.New(
		h.cloud,
		h.kube,
		factory.Core().V1().Services(),
		factory.Core().V1().Nodes(),
		integrationCluster,
		featuregate.NewFeatureGate(),
	)
	if err != nil {
		return err
	}
	factory.Start(h.ctx.Done())
	synced := factory.WaitForCacheSync(h.ctx.Done())
	for typ, ok := range synced {
		if !ok {
			return fmt.Errorf("informer %v failed to sync", typ)
		}
	}
	go ctrl.Run(h.ctx, 1, controllersmetrics.NewControllerManagerMetrics("autoipam-integration"))
	return nil
}

func (h *harness) createLoadBalancerService(name string) *v1.Service {
	h.t.Helper()
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{{
				Name:     "http",
				Port:     80,
				Protocol: v1.ProtocolTCP,
			}},
		},
	}
	created, err := h.kube.CoreV1().Services("default").Create(h.ctx, svc, metav1.CreateOptions{})
	if err != nil {
		h.t.Fatalf("create service: %v", err)
	}
	h.t.Cleanup(func() {
		h.cleanupService(created.Namespace, created.Name, string(created.UID))
	})
	return created
}

func (h *harness) cleanupService(namespace, name, uid string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if uid != "" && h.prismCleanup != nil {
		err := wait.PollUntilContextTimeout(ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
			err := provider.UnreserveIPByContext(ctx, h.prismCleanup, h.subnetExtID, uuid.NewString(), uid)
			if err == nil {
				return true, nil
			}
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				return true, nil
			}
			h.t.Logf("unreserve retry: %v", err)
			return false, nil
		})
		if err != nil {
			h.t.Logf("unreserve cleanup: %v", err)
		}
	}

	_ = h.dyn.Resource(ipAddressPoolGVR).Namespace(h.metalLBNS).Delete(ctx, uid, metav1.DeleteOptions{})

	svc, err := h.kube.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return
	}
	if err != nil {
		h.t.Logf("get service for cleanup: %v", err)
		return
	}
	svc = svc.DeepCopy()
	svc.Finalizers = nil
	if _, err := h.kube.CoreV1().Services(namespace).Update(ctx, svc, metav1.UpdateOptions{}); err != nil && !apierrors.IsNotFound(err) {
		h.t.Logf("strip finalizers: %v", err)
	}
	_ = h.kube.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

func (h *harness) waitForActivation(namespace, name string) *v1.Service {
	h.t.Helper()
	var out *v1.Service
	err := wait.PollUntilContextTimeout(h.ctx, time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		svc, err := h.kube.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if svc.Annotations[constants.LoadBalancerIPAnnotation] == "" {
			return false, nil
		}
		if svc.Annotations[constants.MetalLBRequestedIPsAnnotation] == "" {
			return false, nil
		}
		if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
			return false, nil
		}
		out = svc
		return true, nil
	})
	if err != nil {
		svc, getErr := h.kube.CoreV1().Services(namespace).Get(h.ctx, name, metav1.GetOptions{})
		if getErr == nil {
			h.logManifests("activation timed out", svc)
		}
		h.t.Fatalf("wait for activation: %v", err)
	}
	h.logManifests("activation complete", out)
	return out
}

func (h *harness) getPool(uid string) *unstructured.Unstructured {
	h.t.Helper()
	obj, err := h.dyn.Resource(ipAddressPoolGVR).Namespace(h.metalLBNS).Get(h.ctx, uid, metav1.GetOptions{})
	if err != nil {
		h.t.Fatalf("get pool %s: %v", uid, err)
	}
	return obj
}

// logManifests prints the Service and related AutoIPAM resources as YAML for debugging.
func (h *harness) logManifests(phase string, svc *v1.Service) {
	h.t.Helper()
	h.t.Logf("=== %s ===", phase)
	if svc != nil {
		h.t.Logf("Service %s/%s:\n%s", svc.Namespace, svc.Name, mustYAML(serviceManifest(svc)))
		if uid := string(svc.UID); uid != "" {
			pool, err := h.dyn.Resource(ipAddressPoolGVR).Namespace(h.metalLBNS).Get(h.ctx, uid, metav1.GetOptions{})
			switch {
			case apierrors.IsNotFound(err):
				h.t.Logf("IPAddressPool %s/%s: not found", h.metalLBNS, uid)
			case err != nil:
				h.t.Logf("IPAddressPool %s/%s: get error: %v", h.metalLBNS, uid, err)
			default:
				h.t.Logf("IPAddressPool %s/%s:\n%s", h.metalLBNS, uid, mustYAML(poolManifest(pool)))
			}
		}
	}
}

func serviceManifest(svc *v1.Service) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":        svc.Name,
			"namespace":   svc.Namespace,
			"uid":         string(svc.UID),
			"annotations": svc.Annotations,
			"finalizers":  svc.Finalizers,
		},
		"spec": map[string]any{
			"type":  svc.Spec.Type,
			"ports": svc.Spec.Ports,
		},
		"status": map[string]any{
			"loadBalancer": svc.Status.LoadBalancer,
		},
	}
}

func poolManifest(pool *unstructured.Unstructured) map[string]any {
	if pool == nil {
		return nil
	}
	spec, _, _ := unstructured.NestedMap(pool.Object, "spec")
	return map[string]any{
		"apiVersion": pool.GetAPIVersion(),
		"kind":       pool.GetKind(),
		"metadata": map[string]any{
			"name":      pool.GetName(),
			"namespace": pool.GetNamespace(),
			"labels":    pool.GetLabels(),
		},
		"spec": spec,
	}
}

func mustYAML(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%+v", v)
	}
	return string(b)
}

func (h *harness) listPools() int {
	h.t.Helper()
	list, err := h.dyn.Resource(ipAddressPoolGVR).Namespace(h.metalLBNS).List(h.ctx, metav1.ListOptions{})
	if err != nil {
		h.t.Fatalf("list pools: %v", err)
	}
	return len(list.Items)
}
