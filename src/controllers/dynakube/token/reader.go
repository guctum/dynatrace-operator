package token

import (
	"context"
	dynatracev1beta1 "github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	"github.com/Dynatrace/dynatrace-operator/src/dtclient"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Reader struct {
	apiReader client.Reader
	dynakube  *dynatracev1beta1.DynaKube
}

func NewReader(apiReader client.Reader, dynakube *dynatracev1beta1.DynaKube) Reader {
	return Reader{
		apiReader: apiReader,
		dynakube:  dynakube,
	}
}

func (reader Reader) ReadTokens(ctx context.Context) (Tokens, error) {
	tokens, err := reader.readTokens(ctx)

	if err != nil {
		return nil, err
	}

	err = reader.verifyApiTokenExists(tokens)

	if err != nil {
		return nil, err
	}

	return tokens, nil
}

func (reader Reader) readTokens(ctx context.Context) (Tokens, error) {
	var tokenSecret v1.Secret
	result := make(Tokens)

	err := reader.apiReader.Get(ctx, client.ObjectKey{
		Name:      reader.dynakube.Tokens(),
		Namespace: reader.dynakube.Namespace,
	}, &tokenSecret)

	if err != nil {
		return nil, errors.WithStack(err)
	}

	for tokenType, rawToken := range tokenSecret.Data {
		result[tokenType] = Token{
			Value: string(rawToken),
		}
	}

	return result, nil
}

func (reader Reader) verifyApiTokenExists(tokens Tokens) error {
	apiToken, hasApiToken := tokens[dtclient.DynatraceApiToken]

	if !hasApiToken || len(apiToken.Value) == 0 {
		return errors.New("the API token is missing from the token secret")
	}

	return nil
}

//func (reader Reader) createDynatraceClient(ctx context.Context, tokens Tokens) error {
//	if reader.dtclient != nil {
//		// When unit testing the reader, the dtclient should be set by the unit test
//		// If so, do not recreate an actual dtclient
//		return nil
//	}
//
//	properties := dynakube.NewDynatraceClientProperties(ctx, reader.apiReader, *reader.dynakube, tokens)
//	dtc, err := dynakube.BuildDynatraceClient(*properties)
//
//	if err != nil {
//		return err
//	}
//
//	reader.dtclient = dtc
//
//	return nil
//}
