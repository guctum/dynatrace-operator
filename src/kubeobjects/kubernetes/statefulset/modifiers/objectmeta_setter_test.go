package modifiers

import (
	"testing"

	"github.com/Dynatrace/dynatrace-operator/src/kubeobjects/kubernetes/statefulset"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestObjectMetaSetter(t *testing.T) {
	t.Run("Set obectmeta", func(t *testing.T) {

		om := v1.ObjectMeta{Name: "asd"}

		b := statefulset.Builder{}
		b.AddModifier(
			ObjectMetaSetter{ObjectMeta: om},
		)

		actual := b.Build()
		expected := appsv1.StatefulSet{
			ObjectMeta: om,
		}
		assert.Equal(t, expected, actual)
	})
}
