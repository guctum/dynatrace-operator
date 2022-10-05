package dtclient

import (
	"context"
	"errors"
	"fmt"

	dynatracev1beta1 "github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	"github.com/Dynatrace/dynatrace-operator/src/dtclient"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DynatraceClientReconciler struct {
	kubeClient          client.Client
	dynatraceClientFunc DynatraceClientFunc
}

type DynatraceClientFunc func(properties DynatraceClientProperties) (dtclient.Client, error)

func NewDynatraceClientReconciler(kubeClient client.Client, dtcClientFunc DynatraceClientFunc) *DynatraceClientReconciler {
	return &DynatraceClientReconciler{
		kubeClient:          kubeClient,
		dynatraceClientFunc: dtcClientFunc,
	}
}

func getSecretKey(dynakube dynatracev1beta1.DynaKube) string {
	return dynakube.Namespace + ":" + dynakube.Tokens()
}

func (reconciler *DynatraceClientReconciler) Reconcile(ctx context.Context, dynakube *dynatracev1beta1.DynaKube) (dtclient.Client, TokenMap, error) {
	secret, err := reconciler.getSecret(ctx, dynakube)
	if err != nil {
		return nil, nil, err
	}

	var apiToken string
	var paasToken string
	var dataIngestToken string
	if secret != nil {
		apiToken = string(secret.Data[dtclient.DynatraceApiToken])
		paasToken = string(secret.Data[dtclient.DynatracePaasToken])
		dataIngestToken = string(secret.Data[dtclient.DynatraceDataIngestToken])
	}

	apiTokenConfig, err := createApiTokenConfig(apiToken, dynakube)
	if err != nil {
		return nil, nil, err
	}

	paasTokenConfig := createPaasTokenConfig(paasToken, dynakube)
	if paasTokenConfig == nil {
		apiTokenConfig.Scopes = append(apiTokenConfig.Scopes, dtclient.TokenScopeInstallerDownload)
	}

	addOptionalScopes(apiTokenConfig, dynakube)

	dataIngestTokenConfig := createDataIngestTokenConfig(dataIngestToken, dynakube)

	tokenConfigs := map[string]*TokenConfig{
		dtclient.DynatraceApiToken:        apiTokenConfig,
		dtclient.DynatracePaasToken:       paasTokenConfig,
		dtclient.DynatraceDataIngestToken: dataIngestTokenConfig,
	}

	dtc, err := reconciler.buildDtClient(*secret, dynakube)
	if err != nil {
		return nil, nil, err
	}
	for _, tokenConfig := range tokenConfigs {
		err := tokenConfig.verify(dtc, dynakube)
		if err != nil {
			return nil, nil, err
		}
	}

	return dtc, tokenConfigs, nil
}

func (reconciler *DynatraceClientReconciler) buildDtClient(secret corev1.Secret, dynakube *dynatracev1beta1.DynaKube) (dtclient.Client, error) {
	dtf := reconciler.dynatraceClientFunc
	if dtf == nil {
		dtf = BuildDynatraceClient
	}

	dtc, err := dtf(DynatraceClientProperties{
		ApiReader:           reconciler.kubeClient,
		Secret:              &secret,
		Proxy:               convertProxy(dynakube.Spec.Proxy),
		ApiUrl:              dynakube.Spec.APIURL,
		Namespace:           dynakube.Namespace,
		NetworkZone:         dynakube.Spec.NetworkZone,
		TrustedCerts:        dynakube.Spec.TrustedCAs,
		SkipCertCheck:       dynakube.Spec.SkipCertCheck,
		DisableHostRequests: dynakube.FeatureDisableHostsRequests(),
	})

	if err != nil {
		message := fmt.Sprintf("Failed to create Dynatrace API Client: %s", err)

		setAndLogCondition(dynakube, metav1.Condition{
			Type:    dynatracev1beta1.APITokenConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  dynatracev1beta1.ReasonTokenMissing,
			Message: message,
		})
		return nil, err
	}
	return dtc, err
}

func (reconciler *DynatraceClientReconciler) getSecret(ctx context.Context, dynakube *dynatracev1beta1.DynaKube) (*corev1.Secret, error) {
	secretName := dynakube.Tokens()
	ns := dynakube.GetNamespace()
	secret := corev1.Secret{}

	err := reconciler.kubeClient.Get(ctx, client.ObjectKey{Name: secretName, Namespace: ns}, &secret)

	if k8serrors.IsNotFound(err) {
		message := fmt.Sprintf("Secret '%s' not found", getSecretKey(*dynakube))
		setAndLogCondition(dynakube, metav1.Condition{
			Type:    dynatracev1beta1.APITokenConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  dynatracev1beta1.ReasonTokenSecretNotFound,
			Message: message,
		})
		return nil, err

	} else if err != nil {
		message := fmt.Sprintf("Secret '%s' couldn't be read", getSecretKey(*dynakube))
		setAndLogCondition(dynakube, metav1.Condition{
			Type:    dynatracev1beta1.APITokenConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  dynatracev1beta1.ReasonTokenError,
			Message: message,
		})
		return nil, err
	}
	return &secret, nil
}

func createApiTokenConfig(apiToken string, dynakube *dynatracev1beta1.DynaKube) (*TokenConfig, error) {
	if apiToken == "" {
		msg := fmt.Sprintf("Token %s on secret %s missing", dtclient.DynatraceApiToken, getSecretKey(*dynakube))
		setAndLogCondition(dynakube, metav1.Condition{
			Type:    dynatracev1beta1.APITokenConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  dynatracev1beta1.ReasonTokenMissing,
			Message: msg,
		})
		return nil, errors.New(msg)
	}
	return &TokenConfig{
		Type:      dynatracev1beta1.APITokenConditionType,
		Key:       dtclient.DynatraceApiToken,
		Value:     apiToken,
		Scopes:    []string{},
		Timestamp: dynakube.Status.LastAPITokenProbeTimestamp,
	}, nil
}

func addOptionalScopes(apiTokenConfig *TokenConfig, dynakube *dynatracev1beta1.DynaKube) {
	if !dynakube.FeatureDisableHostsRequests() {
		apiTokenConfig.Scopes = append(apiTokenConfig.Scopes, dtclient.TokenScopeDataExport)
	}

	if dynakube.IsKubernetesMonitoringActiveGateEnabled() &&
		dynakube.FeatureAutomaticKubernetesApiMonitoring() {
		apiTokenConfig.Scopes = append(apiTokenConfig.Scopes,
			dtclient.TokenScopeEntitiesRead,
			dtclient.TokenScopeSettingsRead,
			dtclient.TokenScopeSettingsWrite)
	}

	if dynakube.FeatureActiveGateAuthToken() {
		apiTokenConfig.Scopes = append(apiTokenConfig.Scopes, dtclient.TokenScopeActiveGateTokenCreate)
	}
}

func createPaasTokenConfig(paasToken string, dynakube *dynatracev1beta1.DynaKube) *TokenConfig {
	if paasToken == "" {
		removePaaSTokenCondition(dynakube)
		return nil
	}
	return &TokenConfig{
		Type:      dynatracev1beta1.PaaSTokenConditionType,
		Key:       dtclient.DynatracePaasToken,
		Value:     paasToken,
		Scopes:    []string{dtclient.TokenScopeInstallerDownload},
		Timestamp: dynakube.Status.LastAPITokenProbeTimestamp,
	}

}

func createDataIngestTokenConfig(dataIngestToken string, dynakube *dynatracev1beta1.DynaKube) *TokenConfig {
	if dataIngestToken == "" {
		return nil
	}
	return &TokenConfig{
		Type:      dynatracev1beta1.DataIngestTokenConditionType,
		Key:       dtclient.DynatraceDataIngestToken,
		Value:     dataIngestToken,
		Scopes:    []string{dtclient.TokenScopeMetricsIngest},
		Timestamp: dynakube.Status.LastDataIngestTokenProbeTimestamp,
	}

}

func removePaaSTokenCondition(dynakube *dynatracev1beta1.DynaKube) {
	if meta.FindStatusCondition(dynakube.Status.Conditions, dynatracev1beta1.PaaSTokenConditionType) != nil {
		meta.RemoveStatusCondition(&dynakube.Status.Conditions, dynatracev1beta1.PaaSTokenConditionType)
	}
}

func setAndLogCondition(dynakube *dynatracev1beta1.DynaKube, condition metav1.Condition) error {
	var err error
	if condition.Reason != dynatracev1beta1.ReasonTokenReady {
		log.Info("problem with token detected", "dynakube", dynakube.Name, "token", condition.Type,
			"msg", condition.Message)
		err = errors.New("tokens are not valid")
	}

	condition.LastTransitionTime = metav1.Now()
	meta.SetStatusCondition(&dynakube.Status.Conditions, condition)
	return err
}

func convertProxy(proxy *dynatracev1beta1.DynaKubeProxy) *DynatraceClientProxy {
	if proxy == nil {
		return nil
	}
	return &DynatraceClientProxy{
		Value:     proxy.Value,
		ValueFrom: proxy.ValueFrom,
	}
}
