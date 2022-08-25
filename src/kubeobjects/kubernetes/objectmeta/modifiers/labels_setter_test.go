package modifiers

import (
	"testing"

	"github.com/Dynatrace/dynatrace-operator/src/kubeobjects/kubernetes/objectmeta"
	"github.com/Dynatrace/dynatrace-operator/src/kubeobjects/kubernetes/types"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLabelsSetter(t *testing.T) {
	t.Run("Set labels", func(t *testing.T) {
		labels := types.Labels{"a": "b", "c": "d"}

		b := objectmeta.Builder{}
		b.AddModifier(
			LabelsSetter{Labels: labels},
		)

		actual := b.Build()
		expected := metav1.ObjectMeta{
			Labels: labels,
		}
		assert.Equal(t, expected, actual)
	})
	t.Run("Override labels", func(t *testing.T) {
		labels0 := types.Labels{"a": "b", "c": "d"}
		labels1 := types.Labels{"aa": "b", "cc": "d"}
		b := objectmeta.Builder{}
		b.AddModifier(LabelsSetter{Labels: labels0})
		b.AddModifier(LabelsSetter{Labels: labels1})

		actual := b.Build()
		expected := metav1.ObjectMeta{
			Labels: labels1,
		}
		assert.Equal(t, expected, actual)
	})
}
