package frameworktest

import (
	"github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	apiURL = "https://asj34817.dev.dynatracelabs.com/api"
)

func createClassicFullStackDynakube(namespace string) *v1beta1.DynaKube {
	return &v1beta1.DynaKube{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dynakube",
			Namespace: namespace,
		},
		Spec: v1beta1.DynaKubeSpec{
			APIURL: apiURL,
			OneAgent: v1beta1.OneAgentSpec{
				CloudNativeFullStack: &v1beta1.CloudNativeFullStackSpec{},
			},
		},
	}
}

func createApplicationOnlyDynakubeWithProxy(namespace string, proxy string) *v1beta1.DynaKube {
	dynakube := createApplicationOnlyDynakube(namespace)
	dynakube.Spec.Proxy = &v1beta1.DynaKubeProxy{Value: proxy}
	return dynakube
}

func createApplicationOnlyDynakube(namespace string) *v1beta1.DynaKube {
	return &v1beta1.DynaKube{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dynakube",
			Namespace: namespace,
		},
		Spec: v1beta1.DynaKubeSpec{
			APIURL: apiURL,
			OneAgent: v1beta1.OneAgentSpec{
				ApplicationMonitoring: &v1beta1.ApplicationMonitoringSpec{},
			},
		},
	}
}
