package authorization

import (
	"context"
	"errors"
	"fmt"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/vishu42/openplan/internal/strval"
)

// bootstrapPageSize is how many stores or models one reconcile page asks for.
const bootstrapPageSize = 100

// bootstrap resolves the store and authorization model this process will use,
// creating either only when it is absent.
//
// The refusals here are the point, and each one exists because the alternative
// is to silently decide something nobody chose:
//
//   - two stores with the configured name is ambiguous, and picking one decides
//     which tuples count;
//
//   - two stored models matching the repository's is ambiguous the same way;
//
//   - a matching model is adopted rather than rewritten, so a restart does not
//     mint a new model id that existing tuples were never written against;
//
//   - an id the server returns that isn't strval.SafeOpaque is refused: an id
//     it cannot trust is one it must not pin the process to.
//
//     no store        → create one, write the model
//     store, no match → adopt the store, write a new model version
//     store + match   → adopt both, write nothing
//     two stores      → error
//     two matches     → error
func (auth *Authorization) bootstrap(ctx context.Context, storeName string) error {
	desired, err := desiredModel()
	if err != nil {
		return err
	}

	storeID, err := auth.resolveStore(ctx, storeName)
	if err != nil {
		return err
	}

	modelID, err := auth.resolveModel(ctx, storeID, desired)
	if err != nil {
		return err
	}

	auth.storeID, auth.modelID = storeID, modelID
	return nil
}

// resolveStore finds the single store with this name, or creates it.
func (auth *Authorization) resolveStore(ctx context.Context, storeName string) (string, error) {
	var matches []string
	err := eachPage(errors.New("authorization: list stores repeated a continuation token"), func(token string) (string, error) {
		response, err := auth.server.ListStores(ctx, &openfgav1.ListStoresRequest{
			PageSize:          wrapperspb.Int32(bootstrapPageSize),
			ContinuationToken: token,
		})
		if err != nil {
			return "", fmt.Errorf("authorization: list stores: %w", err)
		}
		for _, store := range response.GetStores() {
			if store.GetName() == storeName {
				if !strval.SafeOpaque(store.GetId()) {
					return "", fmt.Errorf("authorization: store %q has an unsafe id", storeName)
				}
				matches = append(matches, store.GetId())
			}
		}
		return response.GetContinuationToken(), nil
	})
	if err != nil {
		return "", err
	}

	if len(matches) > 1 {
		return "", fmt.Errorf("authorization: found %d stores named %q; bootstrap requires one", len(matches), storeName)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}

	created, err := auth.server.CreateStore(ctx, &openfgav1.CreateStoreRequest{Name: storeName})
	if err != nil {
		return "", fmt.Errorf("authorization: create store %q: %w", storeName, err)
	}
	if !strval.SafeOpaque(created.GetId()) {
		return "", fmt.Errorf("authorization: created store %q has an unsafe id", storeName)
	}
	return created.GetId(), nil
}

// resolveModel adopts the stored model matching the repository's, or writes a
// new version when none does.
func (auth *Authorization) resolveModel(ctx context.Context, storeID string, desired *openfgav1.AuthorizationModel) (string, error) {
	var matches []string
	err := eachPage(errors.New("authorization: list models repeated a continuation token"), func(token string) (string, error) {
		response, err := auth.server.ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{
			StoreId:           storeID,
			PageSize:          wrapperspb.Int32(bootstrapPageSize),
			ContinuationToken: token,
		})
		if err != nil {
			return "", fmt.Errorf("authorization: list models: %w", err)
		}
		for _, message := range response.GetAuthorizationModels() {
			if modelsEqual(desired, message) {
				if !strval.SafeOpaque(message.GetId()) {
					return "", fmt.Errorf("authorization: matching model in store %q has an unsafe id", storeID)
				}
				matches = append(matches, message.GetId())
			}
		}
		return response.GetContinuationToken(), nil
	})
	if err != nil {
		return "", err
	}

	if len(matches) > 1 {
		return "", fmt.Errorf("authorization: found %d models matching the repository model in store %q; selection is ambiguous", len(matches), storeID)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}

	written, err := auth.server.WriteAuthorizationModel(ctx, writeModelRequest(storeID, desired))
	if err != nil {
		return "", fmt.Errorf("authorization: write model in store %q: %w", storeID, err)
	}
	if !strval.SafeOpaque(written.GetAuthorizationModelId()) {
		return "", fmt.Errorf("authorization: written model has an unsafe id")
	}
	return written.GetAuthorizationModelId(), nil
}

// eachPage calls page with each continuation token, starting from none, until
// it hands back none. A provider that keeps handing back the same token would
// loop here forever, so a repeat fails with repeated rather than reading more
// pages.
func eachPage(repeated error, page func(token string) (next string, err error)) error {
	seen := map[string]bool{}
	token := ""
	for {
		next, err := page(token)
		if err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		if seen[next] {
			return repeated
		}
		seen[next] = true
		token = next
	}
}

// modelFromProto converts a stored model into the comparable shape.
//
// Both directions go through protojson because AuthorizationModel is defined by
// the same JSON wire format the protobuf marshals to, so there is one encoding
// to agree on rather than a hand-written field mapping to drift.
// writeModelRequest renders the repository model as a write request. The id is
// not carried over: ids are the server's to mint, and sending one is
// meaningless.
func writeModelRequest(storeID string, model *openfgav1.AuthorizationModel) *openfgav1.WriteAuthorizationModelRequest {
	return &openfgav1.WriteAuthorizationModelRequest{
		StoreId:         storeID,
		SchemaVersion:   model.GetSchemaVersion(),
		TypeDefinitions: model.GetTypeDefinitions(),
		Conditions:      model.GetConditions(),
	}
}
