package frameworktest

import (
	"bytes"
	"context"
	"fmt"
	"github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"os"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
	"strings"
	"testing"
	"time"
)

/*func TestKubernetes(t *testing.T) {
	f1 := features.New("count pod").
		WithLabel("type", "pod-count").
		Assess("pods from kube-system", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			var pods corev1.PodList
			err := cfg.Client().Resources("kube-system").List(context.TODO(), &pods)
			if err != nil {
				t.Fatal(err)
			}
			if len(pods.Items) == 0 {
				t.Fatal("no pods in namespace kube-system")
			}
			return ctx
		}).Feature()

	f2 := features.New("count namespaces").
		WithLabel("type", "ns-count").
		Assess("namespace exist", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			var nspaces corev1.NamespaceList
			err := cfg.Client().Resources().List(context.TODO(), &nspaces)
			if err != nil {
				t.Fatal(err)
			}
			if len(nspaces.Items) == 1 {
				t.Fatal("no other namespace")
			}
			return ctx
		}).Feature()

	// test feature
	testenv.Test(t, f1, f2)
}*/

func TestApplicationOnlyWithProxy(t *testing.T) {
	requiredNamespaces := []string{sampleAppsNamespace, proxyNamespace}

	f1 := features.New("apply application only with proxy set").
		WithLabel("testlevel", "complex").
		WithSetup("Create sample app and proxy namespace", SetupNamespaces(requiredNamespaces)).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			sampleApps := os.DirFS("./sampleapps")
			r := cfg.Client().Resources()
			// disable internet for operator namespace
			err := decoder.DecodeEachFile(ctx, sampleApps,
				"no-internet-*",
				decoder.CreateHandler(r),
				decoder.MutateNamespace(operatorNamespace))
			if err != nil {
				t.Fatal(err)
			}

			// setup proxy
			err = decoder.DecodeEachFile(ctx, sampleApps,
				"proxy*",
				decoder.CreateHandler(r),
				decoder.MutateNamespace(proxyNamespace))
			if err != nil {
				t.Fatal(err)
			}

			return ctx
		}).
		Assess("proxy is running", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// load deployment from file
			err := WaitForDeploymentToBeReady(cfg.Client().Resources(), "nginx-http-proxy", proxyNamespace)
			if err != nil {
				t.Fatal(err)
			}

			// give nginx time to setup
			time.Sleep(time.Second * 5)

			return ctx
		}).
		Assess("deploy dynakube and wait to be ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := cfg.Client().Resources().
				WithNamespace(operatorNamespace)
			proxyUrl := fmt.Sprintf("http://asj34817.%s.svc.cluster.local:9050", proxyNamespace)

			dynakube := createApplicationOnlyDynakubeWithProxy(operatorNamespace, proxyUrl)
			dynakube.Spec.Proxy = &v1beta1.DynaKubeProxy{
				Value: proxyUrl,
			}
			ApplyDynakubeAndCheckIfReady(r, t, dynakube)

			return ctx
		}).
		Assess("creating sample applications", AssessCreateGoSampleApps(sampleAppsNamespace)).
		Assess("check if sample applications start up and have init container attached", AssessCheckIfGoSampleAppsAreRunningAndHaveInitContainerAttached(sampleAppsNamespace)).
		WithTeardown("delete all created objects", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// delete sample apps deployments
			sampleApps := os.DirFS("./sampleapps")

			_ = decoder.DecodeEachFile(ctx, sampleApps,
				"*",
				decoder.DeleteHandler(cfg.Client().Resources(sampleAppsNamespace)),
				decoder.MutateNamespace(sampleAppsNamespace))

			// delete dynakube
			dynakube := createClassicFullStackDynakube(operatorNamespace)
			rDynatrace := cfg.Client().Resources().WithNamespace(operatorNamespace)
			_ = rDynatrace.Delete(ctx, dynakube)

			// wait for oneagent pods
			var oneagentPods corev1.PodList
			err := cfg.Client().Resources().List(context.TODO(), &oneagentPods,
				func(opts *metav1.ListOptions) { opts.LabelSelector = "app.kubernetes.io/managed-by=dynatrace-operator" })
			if err != nil {
				fmt.Println("Oneagent pods already deleted")
			} else {
				_ = WaitForAllPodsToBeDeleted(cfg, oneagentPods)
			}

			// delete no-internet restriction
			_ = decoder.DecodeEachFile(ctx, sampleApps,
				"no-internet-*",
				decoder.DeleteHandler(cfg.Client().Resources()),
				decoder.MutateNamespace(operatorNamespace))

			// wait for namespace to be really deleted
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: sampleAppsNamespace,
				},
			}
			err = wait.For(conditions.New(cfg.Client().Resources()).ResourceDeleted(namespace), wait.WithTimeout(timeout))
			if err != nil {
				t.Fatal(err)
			}

			return ctx
		}).
		WithTeardown("delete sample and proxy namespace", DeleteNamespaces(requiredNamespaces)).
		Feature()
	testenv.Test(t, f1)
}

func TestCloudNativeFullStack(t *testing.T) {
	requiredNamespaces := []string{sampleAppsNamespace}

	f2 := features.New("apply simple cloud native monitoring").
		WithLabel("testlevel", "basic").
		WithSetup("create required namespaces", SetupNamespaces(requiredNamespaces)).
		Assess("create dynakube and wait till ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			dynakube := createClassicFullStackDynakube(operatorNamespace)

			ApplyDynakubeAndCheckIfReady(cfg.Client().Resources(), t, dynakube)

			return ctx
		}).
		Assess("host monitoring pods are ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// wait till there is a ds with label app.kubernetes.io/name=oneagent and check if all pods are running
			r := cfg.Client().Resources().
				WithNamespace(operatorNamespace)

			// create daemonset with name dynakube-oneagent
			ds := CreateDaemonset("dynakube-oneagent", "dynatrace")
			err := WaitForDaemonsetToBeReady(r, ds)
			if err != nil {
				t.Fail()
			}

			return ctx
		}).
		Assess("latest oneagent version was downloaded by CSI Driver", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// get all csi driver pods
			r := cfg.Client().Resources().
				WithNamespace(operatorNamespace)

			csiDriverPods, err := GetAllCSIDriverPods(ctx, r)
			if err != nil {
				t.Fatal(err)
			}

			clientset, err := kubernetes.NewForConfig(cfg.Client().RESTConfig())
			if err != nil {
				t.Fatal(err)
			}

			// check for all csidriver pods if the latest oneagent version was downloaded
			for _, pod := range csiDriverPods {
				// create stdout writer
				retries := 5
				agentDownloaded := false
				for retries > 0 {
					retries--
					stdout := &bytes.Buffer{}
					stderr := &bytes.Buffer{}

					err := ExecCmdExample(clientset,
						cfg.Client().RESTConfig(),
						pod.Name,
						"server",
						"ls /data/asj34817/bin/",
						nil,
						stdout,
						stderr)
					if err != nil {
						t.Fail()
					}
					output := stdout.String()
					if output != "" {
						agentDownloaded = true
					}
					if agentDownloaded {
						fmt.Printf("Current downloaded oneagent version is: %s, installed on node %s\n", strings.TrimSpace(output), pod.Spec.NodeName)
						break
					} else {
						fmt.Printf("Waiting for provisioner on node %s to download the latest oneagent version\n", pod.Spec.NodeName)
						time.Sleep(time.Second * 10)
					}
				}
				if !agentDownloaded {
					fmt.Println("Oneagent was not downloaded by the provisioner on node", pod.Spec.NodeName)
					t.Fail()
				}
			}

			return ctx
		}).
		Assess("hosts are monitored in dynatrace", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// todo call api and check if hosts are monitored

			return ctx
		}).
		Assess("creating sample applications", AssessCreateGoSampleApps(sampleAppsNamespace)).
		Assess("check if sample applications start up and have init container attached", AssessCheckIfGoSampleAppsAreRunningAndHaveInitContainerAttached(sampleAppsNamespace)).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// delete sample apps deployments
			sampleApps := os.DirFS("./sampleapps")

			_ = decoder.DecodeEachFile(ctx, sampleApps,
				"go-*",
				decoder.DeleteHandler(cfg.Client().Resources(sampleAppsNamespace)),
				decoder.MutateNamespace("default"))

			// give time to delete before csi driver destroyed
			time.Sleep(time.Second * 10)

			// delete dynakube
			dynakube := createClassicFullStackDynakube(operatorNamespace)
			rDynatrace := cfg.Client().Resources().WithNamespace(operatorNamespace)
			rDynatrace.Delete(ctx, dynakube)

			// wait for oneagent pods
			var oneagentPods corev1.PodList
			err := cfg.Client().Resources().List(context.TODO(), &oneagentPods,
				func(opts *metav1.ListOptions) { opts.LabelSelector = "app.kubernetes.io/managed-by=dynatrace-operator" })
			if err != nil {
				fmt.Println("Oneagent pods already deleted")
			} else {
				_ = WaitForAllPodsToBeDeleted(cfg, oneagentPods)
			}

			// todo ev. check if csi garbage collector did its job

			return ctx
		}).WithTeardown("delete sample and proxy namespace", DeleteNamespaces(requiredNamespaces)).
		Feature()

	// test feature
	testenv.Test(t, f2)
}
