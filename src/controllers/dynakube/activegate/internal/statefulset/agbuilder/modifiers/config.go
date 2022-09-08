package modifiers

import (
	dynatracev1beta1 "github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	"github.com/Dynatrace/dynatrace-operator/src/controllers/dynakube/activegate/capability"
	"github.com/Dynatrace/dynatrace-operator/src/controllers/dynakube/activegate/internal/statefulset/agbuilder"
	"github.com/Dynatrace/dynatrace-operator/src/logger"
	corev1 "k8s.io/api/core/v1"
)

type volumeModifier interface {
	getVolumes() []corev1.Volume
}

type volumeMountModifier interface {
	getVolumeMounts() []corev1.VolumeMount
}

type envModifier interface {
	getEnvs() []corev1.EnvVar
}

var (
	log = logger.NewDTLogger().WithName("activegate-statefulset-builder")
)

func GetAllModifiers(dynakube dynatracev1beta1.DynaKube, capability capability.Capability) []agbuilder.Modifier {
	return []agbuilder.Modifier{
		NewAuthTokenModifier(dynakube),
		NewCertificatesModifier(dynakube),
		NewCustomPropertiesModifier(dynakube, capability),
		NewExtensionControllerModifier(dynakube, capability),
		NewProxyModifier(dynakube),
		NewRawImageModifier(dynakube),
		NewReadOnlyModifier(dynakube),
		NewStatsdModifier(dynakube, capability),
	}
}
