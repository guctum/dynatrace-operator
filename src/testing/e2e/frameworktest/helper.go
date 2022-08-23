package frameworktest

import (
	"context"
	"fmt"
	"github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/features"
	"testing"
	"time"
)

const timeout = time.Minute * 2

func WaitForAllPodsToBeReady(cfg *envconf.Config, pods corev1.PodList) error {
	return wait.For(conditions.New(cfg.Client().Resources()).ResourcesMatch(&pods, func(object k8s.Object) bool {
		allReady := true
		for _, c := range object.(*corev1.Pod).Status.Conditions {
			if c.Type == corev1.PodReady && c.Status != corev1.ConditionTrue {
				allReady = false
				break
			}
		}
		return allReady
	}), wait.WithTimeout(timeout))
}

func WaitForAllPodsToBeDeleted(cfg *envconf.Config, pods corev1.PodList) error {
	return wait.For(conditions.New(cfg.Client().Resources()).ResourcesDeleted(&pods), wait.WithTimeout(timeout))
}

func WaitForDynakubeToBeReady(r *resources.Resources, dynakube v1beta1.DynaKube) error {
	_ = v1beta1.AddToScheme(r.GetScheme())

	return wait.For(conditions.New(r).ResourceMatch(&dynakube, func(object k8s.Object) bool {
		return object.(*v1beta1.DynaKube).Status.Phase == v1beta1.Running
	}), wait.WithTimeout(timeout))
}

func WaitForDaemonsetToBeReady(r *resources.Resources, daemonset appsv1.DaemonSet) error {
	return wait.For(conditions.New(r).ResourceMatch(&daemonset, func(object k8s.Object) bool {
		return object.(*appsv1.DaemonSet).Status.NumberReady == object.(*appsv1.DaemonSet).Status.DesiredNumberScheduled
	}), wait.WithTimeout(timeout))
}

func WaitForDeploymentToBeReady(r *resources.Resources, name, namespace string) error {
	sampleDeployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	return wait.For(conditions.New(r).ResourceMatch(&sampleDeployment, func(object k8s.Object) bool {
		return object.(*appsv1.Deployment).Status.Replicas == object.(*appsv1.Deployment).Status.ReadyReplicas
	}), wait.WithTimeout(timeout))
}

func GetAllCSIDriverPods(ctx context.Context, r *resources.Resources) ([]corev1.Pod, error) {
	var pods corev1.PodList
	err := r.List(ctx, &pods)
	if err != nil {
		return nil, err
	}

	var csiDriverPods []corev1.Pod
	for _, pod := range pods.Items {
		if pod.Labels["app.kubernetes.io/component"] == "csi-driver" {
			csiDriverPods = append(csiDriverPods, pod)
		}
	}

	return csiDriverPods, nil
}

func CreateDaemonset(name string, namespace string) appsv1.DaemonSet {
	return appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dynakube-oneagent",
			Namespace: "dynatrace",
		},
	}
}

func CreateNamespaceIfNotExists(name string) env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		namespace := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		r := cfg.Client().Resources()
		_ = r.Create(ctx, &namespace)
		cfg.WithNamespace(name) // set env config default namespace
		return ctx, nil
	}
}

func DeleteNamespaces(namespaces []string) features.Func {
	return func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		var err error
		for _, namespace := range namespaces {
			ctx, err = envfuncs.DeleteNamespace(namespace)(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Println("Namespace " + namespace + " deleted")
		}
		return ctx
	}
}

func ApplyDynakubeAndCheckIfReady(r *resources.Resources, t *testing.T, dynakube *v1beta1.DynaKube) {
	if err := v1beta1.AddToScheme(r.GetScheme()); err != nil {
		t.Fatal(err)
	}
	if err := r.Create(context.TODO(), dynakube); err != nil {
		t.Fatal(err)
	}

	err := WaitForDynakubeToBeReady(r, *dynakube)
	if err != nil {
		t.Fail()
	}
}
