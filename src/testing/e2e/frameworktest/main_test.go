package frameworktest

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"sigs.k8s.io/e2e-framework/klient/conf"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"testing"
	"time"
)

var (
	testenv env.Environment
)

const operatorNamespace = "dynatrace"
const sampleAppsNamespace = "test"
const proxyNamespace = "proxy"

func TestMain(m *testing.M) {
	testenv = env.New()
	//kindClusterName := envconf.RandomName("my-cluster", 16)

	if os.Getenv("REAL_CLUSTER") == "true" {
		path := conf.ResolveKubeConfigFile()
		cfg := envconf.NewWithKubeConfig(path)
		testenv = env.NewWithConfig(cfg)

		testenv.Setup(
			CreateNamespaceIfNotExists(operatorNamespace),
			createOperatorWithCSIDriver(true),
			createTokenSecret(),
		)
		testenv.Finish(
			createOperatorWithCSIDriver(false),
			envfuncs.DeleteNamespace(operatorNamespace),
		)
	} /*else {
		// Use pre-defined environment funcs to create a kind cluster prior to test run
		testenv.Setup(
			envfuncs.CreateKindCluster(kindClusterName),
			createOperatorWithCSIDriver(true),
		)

		// Use pre-defined environment funcs to teardown kind cluster after tests
		testenv.Finish(
			createOperatorWithCSIDriver(false),
			envfuncs.DeleteNamespace(namespace),
			envfuncs.DestroyKindCluster(kindClusterName),
		)
	}*/
	// launch package tests
	os.Exit(testenv.Run(m))
}

func createTokenSecret() env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		apiToken := os.Getenv("API_TOKEN")
		if apiToken == "" {
			return ctx, fmt.Errorf("API_TOKEN is not set")
		}
		paasToken := os.Getenv("PAAS_TOKEN")
		if paasToken == "" {
			return ctx, fmt.Errorf("PAAS_TOKEN is not set")
		}

		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dynakube",
				Namespace: "dynatrace",
			},
			Data: map[string][]byte{
				"apiToken":  []byte(apiToken),
				"paasToken": []byte(paasToken),
			},
		}

		r, err := resources.New(cfg.Client().RESTConfig())
		if err != nil {
			return ctx, err
		}

		return ctx, r.Create(ctx, secret)
	}
}

func createOperatorWithCSIDriver(create bool) env.Func {

	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		r, err := resources.New(cfg.Client().RESTConfig())
		if err != nil {
			return ctx, err
		}

		// read config/deploy/kubernetes/kubernetes-all.yaml from file
		kubernetesAllYaml, err := os.Open("../../../../config/deploy/kubernetes/kubernetes-all.yaml")
		if err != nil {
			return ctx, err
		}
		//defer kubernetesAllYaml.Close()

		// decode and create a stream of YAML or JSON documents from an io.Reader
		if create {
			err = decoder.DecodeEach(ctx, kubernetesAllYaml, decoder.CreateHandler(r))
		} else {
			_ = decoder.DecodeEach(ctx, kubernetesAllYaml, decoder.DeleteHandler(r))
		}

		if err != nil {
			return ctx, err
		}

		// give the controllers time to create the pods, wait 1 sec
		time.Sleep(1 * time.Second)

		namespace := "dynatrace"
		// wait till all pods in dynatrace namespace are ready
		// find all pods in dynatrace namespace
		var pods v1.PodList
		cfg.Client().Resources(namespace).List(context.TODO(), &pods)

		if create {
			err = WaitForAllPodsToBeReady(cfg, pods)
		} else {
			err = WaitForAllPodsToBeDeleted(cfg, pods)
		}

		return ctx, err
	}
}
