package dynakube

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	dynatracev1beta1 "github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	"github.com/Dynatrace/dynatrace-operator/src/controllers/dynakube/activegate"
	"github.com/Dynatrace/dynatrace-operator/src/controllers/dynakube/apimonitoring"
	"github.com/Dynatrace/dynatrace-operator/src/controllers/dynakube/dtpullsecret"
	"github.com/Dynatrace/dynatrace-operator/src/controllers/dynakube/istio"
	"github.com/Dynatrace/dynatrace-operator/src/controllers/dynakube/oneagent"
	"github.com/Dynatrace/dynatrace-operator/src/controllers/dynakube/oneagent/daemonset"
	"github.com/Dynatrace/dynatrace-operator/src/controllers/dynakube/status"
	"github.com/Dynatrace/dynatrace-operator/src/controllers/dynakube/version"
	"github.com/Dynatrace/dynatrace-operator/src/dtclient"
	dtingestendpoint "github.com/Dynatrace/dynatrace-operator/src/ingestendpoint"
	"github.com/Dynatrace/dynatrace-operator/src/initgeneration"
	"github.com/Dynatrace/dynatrace-operator/src/kubeobjects"
	"github.com/Dynatrace/dynatrace-operator/src/mapper"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const shortUpdateInterval = 30 * time.Second

func Add(mgr manager.Manager, _ string) error {
	return NewController(mgr).SetupWithManager(mgr)
}

// NewController returns a new ReconcileDynaKube
func NewController(mgr manager.Manager) *DynakubeController {
	return NewDynaKubeController(mgr.GetClient(), mgr.GetAPIReader(), mgr.GetScheme(), BuildDynatraceClient, mgr.GetConfig())
}

func NewDynaKubeController(c client.Client, apiReader client.Reader, scheme *runtime.Scheme, dtcBuildFunc DynatraceClientFunc, config *rest.Config) *DynakubeController {
	return &DynakubeController{
		client:            c,
		apiReader:         apiReader,
		scheme:            scheme,
		fs:                afero.Afero{Fs: afero.NewOsFs()},
		dtcBuildFunc:      dtcBuildFunc,
		config:            config,
		operatorPodName:   os.Getenv("POD_NAME"),
		operatorNamespace: os.Getenv("POD_NAMESPACE"),
	}
}

func (controller *DynakubeController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dynatracev1beta1.DynaKube{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&appsv1.DaemonSet{}).
		Complete(controller)
}

// DynakubeController reconciles a DynaKube object
type DynakubeController struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client            client.Client
	apiReader         client.Reader
	scheme            *runtime.Scheme
	fs                afero.Afero
	dtcBuildFunc      DynatraceClientFunc
	config            *rest.Config
	operatorPodName   string
	operatorNamespace string
}

type DynatraceClientFunc func(properties DynatraceClientProperties) (dtclient.Client, error)

// Reconcile reads that state of the cluster for a DynaKube object and makes changes based on the state read
// and what is in the DynaKube.Spec
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (controller *DynakubeController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log.Info("reconciling DynaKube", "namespace", request.Namespace, "name", request.Name)

	dynakube := dynatracev1beta1.DynaKube{ObjectMeta: metav1.ObjectMeta{Name: request.Name, Namespace: request.Namespace}}
	dkMapper := mapper.NewDynakubeMapper(ctx, controller.client, controller.apiReader, controller.operatorNamespace, &dynakube)
	err := errors.WithStack(controller.client.Get(ctx, client.ObjectKey{Name: dynakube.Name, Namespace: dynakube.Namespace}, &dynakube))
	if k8serrors.IsNotFound(err) {
		err = dkMapper.UnmapFromDynaKube()
		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	oldStatus := dynakube.Status
	updated := controller.reconcileIstio(&dynakube)
	if updated {
		log.Info("Istio: objects updated")
		// dkState.RequeueAfter = shortUpdateInterval WHY ?????
	}

	err = controller.reconcileDynaKube(ctx, &dynakube, &dkMapper)

	if err != nil {
		var serr dtclient.ServerError
		if ok := errors.As(err, &serr); ok && serr.Code == http.StatusTooManyRequests {
			// should we set the phase to error ?
			log.Info("request limit for Dynatrace API reached! Next reconcile in one minute")
			return reconcile.Result{RequeueAfter: 1 * time.Minute}, nil
		}

		if dynakube.Status.SetPhase(dynatracev1beta1.Error) {
			if errClient := controller.updateCR(ctx, &dynakube); errClient != nil {
				return reconcile.Result{}, fmt.Errorf("failed to update CR after failure, original, %s, then: %w", err, errClient)
			}
		}
		return reconcile.Result{}, err
	}

	if kubeobjects.IsDifferent(oldStatus, dynakube.Status) {
		if err := controller.updateCR(ctx, &dynakube); err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func (controller *DynakubeController) reconcileIstio(dynakube *dynatracev1beta1.DynaKube) bool {
	var err error
	updated := false

	if dynakube.Spec.EnableIstio {
		updated, err = istio.NewIstioReconciler(controller.config, controller.scheme).ReconcileIstio(dynakube)
		if err != nil {
			// If there are errors log them, but move on.
			log.Info("Istio: failed to reconcile objects", "error", err)
		}
	}

	return updated
}

func (controller *DynakubeController) reconcileDynaKube(ctx context.Context, dynakube *dynatracev1beta1.DynaKube, dkMapper *mapper.DynakubeMapper) error {
	dtcReconciler := DynatraceClientReconciler{
		Client:              controller.client,
		DynatraceClientFunc: controller.dtcBuildFunc,
	}
	dtc, err := dtcReconciler.Reconcile(ctx, dynakube)
	if err != nil {
		log.Info("failed to create dynatrace client")
		return err
	}

	if !dtcReconciler.ValidTokens {
		log.Info("tokens are invalid")
		return err
	}

	err = status.SetDynakubeStatus(dynakube, status.Options{
		Dtc:       dtc,
		ApiClient: controller.apiReader,
	})
	if err != nil {
		log.Info("could not set Dynakube status")
		return err
	}

	err = dtpullsecret.
		NewReconciler(controller.client, controller.apiReader, controller.scheme, dynakube, dtcReconciler.ApiToken, dtcReconciler.PaasToken).
		Reconcile()
	if err != nil {
		log.Info("could not reconcile Dynatrace pull secret")
		return err
	}

	err = version.ReconcileVersions(ctx, dynakube, controller.apiReader, controller.fs, version.GetImageVersion)
	if err != nil {
		log.Info("could not reconcile component versions")
		return err
	}

	err = controller.reconcileActiveGate(ctx, dynakube, dtc)
	if err != nil {
		log.Info("could not reconcile ActiveGate")
		return err
	}

	err = controller.reconcileOneAgent(ctx, dynakube)
	if err != nil {
		log.Info("could not reconcile OneAgent")
		return err
	}

	endpointSecretGenerator := dtingestendpoint.NewEndpointSecretGenerator(controller.client, controller.apiReader, dynakube.Namespace)
	if dynakube.NeedAppInjection() {
		if err = dkMapper.MapFromDynakube(); err != nil {
			log.Info("update of a map of namespaces failed")
			return err
		}

		err = initgeneration.NewInitGenerator(controller.client, controller.apiReader, dynakube.Namespace).GenerateForDynakube(ctx, dynakube)
		if err != nil {
			log.Info("failed to generate init secret")
			return err
		}

		err = endpointSecretGenerator.GenerateForDynakube(ctx, dynakube)
		if err != nil {
			log.Info("failed to generate data-ingest secret")
			return err
		}

		if dynakube.ApplicationMonitoringMode() {
			dynakube.Status.SetPhase(dynatracev1beta1.Running)
		}
	} else {
		if err := dkMapper.UnmapFromDynaKube(); err != nil {
			log.Info("could not unmap dynakube from namespace")
			return err
		}
		err = endpointSecretGenerator.RemoveEndpointSecrets(ctx, dynakube)
		if err != nil {
			log.Info("could not remove data-ingest secret")
			return err
		}
	}
	return nil
}

func (controller *DynakubeController) reconcileOneAgent(ctx context.Context, dynakube *dynatracev1beta1.DynaKube) (err error) {
	if dynakube.HostMonitoringMode() {
		err = oneagent.NewOneAgentReconciler(
			controller.client, controller.apiReader, controller.scheme, dynakube, daemonset.DeploymentTypeHostMonitoring,
		).Reconcile(ctx)
		if err != nil {
			return err
		}
		log.Info("reconciled host-monitoring")
	} else if dynakube.CloudNativeFullstackMode() {
		err = oneagent.NewOneAgentReconciler(
			controller.client, controller.apiReader, controller.scheme, dynakube, daemonset.DeploymentTypeCloudNative,
		).Reconcile(ctx)
		if err != nil {
			return err
		}
		log.Info("reconciled cloud-native")
	} else if dynakube.ClassicFullStackMode() {
		err = oneagent.NewOneAgentReconciler(
			controller.client, controller.apiReader, controller.scheme, dynakube, daemonset.DeploymentTypeFullStack,
		).Reconcile(ctx)
		if err != nil {
			return err
		}
		log.Info("reconciled classic-fullstack")
	} else {
		controller.removeOneAgentDaemonSet(dynakube)
	}
	return err
}

func updatePhaseIfChanged(instance *dynatracev1beta1.DynaKube, newPhase dynatracev1beta1.DynaKubePhaseType) bool {
	if instance.Status.Phase == newPhase {
		return false
	}
	instance.Status.Phase = newPhase
	return true
}

func (controller *DynakubeController) removeOneAgentDaemonSet(dynakube *dynatracev1beta1.DynaKube) error {
	ds := appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: dynakube.OneAgentDaemonsetName(), Namespace: dynakube.Namespace}}
	return kubeobjects.Delete(context.TODO(), controller.client, &ds)
}

func (controller *DynakubeController) reconcileActiveGate(ctx context.Context, dynakube *dynatracev1beta1.DynaKube, dtc dtclient.Client) error {
	reconciler := activegate.NewReconciler(ctx, controller.client, controller.apiReader, controller.scheme, dynakube, dtc)
	_, err := reconciler.Reconcile() // TODO fix Reconcile interface usage

	if err != nil {
		return errors.WithMessage(err, "failed to reconcile ActiveGate")
	}
	controller.setupAutomaticApiMonitoring(dynakube, dtc)

	return nil
}

func (controller *DynakubeController) setupAutomaticApiMonitoring(dynakube *dynatracev1beta1.DynaKube, dtc dtclient.Client) {
	if dynakube.Status.KubeSystemUUID != "" &&
		dynakube.FeatureAutomaticKubernetesApiMonitoring() &&
		dynakube.IsKubernetesMonitoringActiveGateEnabled() {

		clusterLabel := dynakube.FeatureAutomaticKubernetesApiMonitoringClusterName()
		if clusterLabel == "" {
			clusterLabel = dynakube.Name
		}

		err := apimonitoring.NewReconciler(dtc, clusterLabel, dynakube.Status.KubeSystemUUID).
			Reconcile()
		if err != nil {
			log.Error(err, "could not create setting")
		}
	}
}

func (controller *DynakubeController) updateCR(ctx context.Context, dynakube *dynatracev1beta1.DynaKube) error {
	dynakube.Status.UpdatedTimestamp = metav1.Now()
	err := controller.client.Status().Update(ctx, dynakube)
	if err != nil && k8serrors.IsConflict(err) {
		// OneAgent reconciler already updates instance which leads to conflict here
		// Only print info in that event
		log.Info("could not update instance due to conflict")
		return nil
	}
	return errors.WithStack(err)
}
