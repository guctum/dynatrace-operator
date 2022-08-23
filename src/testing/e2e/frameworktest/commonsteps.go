package frameworktest

import (
	"context"
	"fmt"
	"github.com/Dynatrace/dynatrace-operator/src/webhook"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
	"testing"
)

func SetupNamespaces(namespaces []string) features.Func {
	return func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		var err error
		for _, namespace := range namespaces {
			ctx, err = CreateNamespaceIfNotExists(namespace)(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Println("Namespace", namespace, "created")
		}
		return ctx
	}
}

func AssessCreateGoSampleApps(sampleAppsNamespace string) features.Func {
	return func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		r := cfg.Client().Resources()

		// read config/deploy/kubernetes/kubernetes-all.yaml from file
		sampleApps := os.DirFS("./sampleapps")

		err := decoder.DecodeEachFile(ctx, sampleApps,
			"go-*",
			decoder.CreateHandler(r),
			decoder.MutateNamespace(sampleAppsNamespace))
		if err != nil {
			t.Fatal(err)
		}

		return ctx
	}
}

func AssessCheckIfGoSampleAppsAreRunningAndHaveInitContainerAttached(sampleAppsNamespace string) features.Func {
	return func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		// wait till sample pods are ready
		err := WaitForDeploymentToBeReady(cfg.Client().Resources().WithNamespace(sampleAppsNamespace), "test-go-glibc", sampleAppsNamespace)
		if err != nil {
			t.Fatal(err)
		}

		// check if init container is there
		var pods corev1.PodList
		err = cfg.Client().Resources(sampleAppsNamespace).List(context.TODO(), &pods,
			func(opts *metav1.ListOptions) { opts.LabelSelector = "app=go-glibc" })

		for _, pod := range pods.Items {
			initContainerFound := false
			for _, container := range pod.Spec.InitContainers {
				if container.Name == webhook.InstallContainerName {
					initContainerFound = true
				}
			}
			if !initContainerFound {
				fmt.Println("Init container not found on pod", pod.Name)
				t.Fail()
			} else {
				fmt.Println("Init container found on pod", pod.Name)
			}
		}

		return ctx
	}
}
