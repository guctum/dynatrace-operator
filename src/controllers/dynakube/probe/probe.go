package probe

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Is this needed, or just call, metav1.Now() where needed?
// Having separate interface is good for mocking
type Prober interface {
	SetNow()

	Now() *metav1.Time
	IsOutdated(last *metav1.Time, threshold time.Duration) bool
}

var _ Prober = &ReconcileProbe{}

type ReconcileProbe struct {
	startOfReconcile metav1.Time
}

func (rec *ReconcileProbe) SetNow() {
	rec.startOfReconcile = metav1.Now()
}

func (rec ReconcileProbe) Now() *metav1.Time {
	return rec.startOfReconcile.DeepCopy()
}

func (rec ReconcileProbe) IsOutdated(last *metav1.Time, threshold time.Duration) bool {
	return last == nil || last.Add(threshold).Before(rec.startOfReconcile.Time)
}
