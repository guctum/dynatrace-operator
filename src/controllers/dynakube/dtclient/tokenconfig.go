package dtclient

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	dynatracev1beta1 "github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	"github.com/Dynatrace/dynatrace-operator/src/dtclient"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TokenConfig struct {
	Type       string
	Key, Value string
	Scopes     []string
	Timestamp  *metav1.Time
}

type TokenMap map[string]*TokenConfig

func (token TokenConfig) verifyFormat(dynakube *dynatracev1beta1.DynaKube) error {
	if strings.TrimSpace(token.Value) != token.Value {
		return setAndLogCondition(dynakube, metav1.Condition{
			Type:    token.Type,
			Status:  metav1.ConditionFalse,
			Reason:  dynatracev1beta1.ReasonTokenUnauthorized,
			Message: fmt.Sprintf("Token on secret %s has leading and/or trailing spaces", getSecretKey(*dynakube)),
		})
	}
	return nil
}

func (token *TokenConfig) verifyTime(dynakube *dynatracev1beta1.DynaKube) error {
	now := metav1.Now()
	// At this point, we can query the Dynatrace API to verify whether our tokens are correct. To avoid excessive requests,
	// we wait at least 5 mins between proves.
	if token.Timestamp != nil && now.Time.Before((*token.Timestamp).Add(5*time.Minute)) {
		oldCondition := meta.FindStatusCondition(dynakube.Status.Conditions, token.Type)
		if oldCondition.Reason != dynatracev1beta1.ReasonTokenReady {
			return errors.New("tokens are not valid")
		}
		return nil
	}
	token.Timestamp = &now
	return nil
}

func (token TokenConfig) verifyScopes(dtc dtclient.Client, dynakube *dynatracev1beta1.DynaKube) error {
	tokenScopes, err := dtc.GetTokenScopes(token.Value)

	isUnauthorized, err := token.handleUnauthorizedError(err, dynakube)
	if isUnauthorized {
		return err
	}

	err = token.handleUnknownError(err, dynakube)
	if err != nil {
		return err
	}

	return token.handleMissingScopes(tokenScopes, dynakube)
}

func (token TokenConfig) handleUnauthorizedError(err error, dynakube *dynatracev1beta1.DynaKube) (bool, error) {
	var serr dtclient.ServerError
	if ok := errors.As(err, &serr); ok && serr.Code == http.StatusUnauthorized {
		return true, setAndLogCondition(dynakube, metav1.Condition{
			Type:    token.Type,
			Status:  metav1.ConditionFalse,
			Reason:  dynatracev1beta1.ReasonTokenUnauthorized,
			Message: fmt.Sprintf("Token on secret %s unauthorized", getSecretKey(*dynakube)),
		})
	}
	return false, err
}

func (token TokenConfig) handleUnknownError(err error, dynakube *dynatracev1beta1.DynaKube) error {
	if err != nil {
		return setAndLogCondition(dynakube, metav1.Condition{
			Type:    token.Type,
			Status:  metav1.ConditionFalse,
			Reason:  dynatracev1beta1.ReasonTokenError,
			Message: fmt.Sprintf("error when querying token on secret %s: %v", getSecretKey(*dynakube), err),
		})
	}
	return nil
}

func (token TokenConfig) handleMissingScopes(scopes dtclient.TokenScopes, dynakube *dynatracev1beta1.DynaKube) error {
	missingScopes := make([]string, 0)
	for _, s := range token.Scopes {
		if !scopes.Contains(s) {
			missingScopes = append(missingScopes, s)
		}
	}

	if len(missingScopes) > 0 {
		return setAndLogCondition(dynakube, metav1.Condition{
			Type:    token.Type,
			Status:  metav1.ConditionFalse,
			Reason:  dynatracev1beta1.ReasonTokenScopeMissing,
			Message: fmt.Sprintf("Token on secret %s missing scopes [%s]", getSecretKey(*dynakube), strings.Join(missingScopes, ", ")),
		})

	}
	return nil
}

func (token *TokenConfig) verify(dtc dtclient.Client, dynakube *dynatracev1beta1.DynaKube) error {
	err := token.verifyFormat(dynakube)
	if err != nil {
		return err
	}

	err = token.verifyTime(dynakube)
	if err != nil {
		return err
	}

	err = token.verifyScopes(dtc, dynakube)
	if err != nil {
		return err
	}

	return setAndLogCondition(dynakube, metav1.Condition{
		Type:    token.Type,
		Status:  metav1.ConditionTrue,
		Reason:  dynatracev1beta1.ReasonTokenReady,
		Message: "Ready",
	})
}
