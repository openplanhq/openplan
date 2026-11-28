package authorization

import (
	"bytes"
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/server"
	"github.com/openfga/openfga/pkg/storage/memory"
	"gopkg.in/yaml.v3"
)

// modelTestFile is the model's permission matrix, in the format `fga model
// test` reads. Running it here puts it under `go test ./...`, so CI runs it on
// every change, and against the OpenFGA this module links rather than
// whichever version a locally installed CLI bundles.
const modelTestFile = "authorization-model-tests.fga.yaml"

// modelTests is the part of the CLI's test format the matrix uses. Decoding is
// strict, so a key this runner does not implement (list_objects, list_users,
// a tuple condition) fails the test instead of being skipped. Extend the
// runner when the matrix needs one.
type modelTests struct {
	Name      string           `yaml:"name"`
	ModelFile string           `yaml:"model_file"`
	Tuples    []modelTestTuple `yaml:"tuples"`
	Tests     []struct {
		Name  string `yaml:"name"`
		Check []struct {
			User       string          `yaml:"user"`
			Object     string          `yaml:"object"`
			Assertions map[string]bool `yaml:"assertions"`
		} `yaml:"check"`
	} `yaml:"tests"`
}

type modelTestTuple struct {
	User     string `yaml:"user"`
	Relation string `yaml:"relation"`
	Object   string `yaml:"object"`
}

func loadModelTests(t *testing.T) modelTests {
	t.Helper()
	content, err := os.ReadFile(modelTestFile)
	if err != nil {
		t.Fatalf("read %s: %v", modelTestFile, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	var matrix modelTests
	if err := decoder.Decode(&matrix); err != nil {
		t.Fatalf("decode %s: %v", modelTestFile, err)
	}
	// The CLI tests the file named here and this runner tests the embedded
	// model, so they agree only while both are authorization-model.fga.
	if matrix.ModelFile != "./authorization-model.fga" {
		t.Fatalf("%s tests model_file %q, want ./authorization-model.fga, the model the server embeds", modelTestFile, matrix.ModelFile)
	}
	return matrix
}

// TestAuthorizationModelMatrix runs every check in the matrix against the
// embedded model, the same way `fga model test` does.
func TestAuthorizationModelMatrix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	matrix := loadModelTests(t)

	datastore := memory.New()
	engine, err := server.NewServerWithOpts(server.WithDatastore(datastore))
	if err != nil {
		datastore.Close()
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() {
		engine.Close()
		datastore.Close()
	})
	store, err := engine.CreateStore(ctx, &openfgav1.CreateStoreRequest{Name: "model-matrix"})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	model, err := desiredModel()
	if err != nil {
		t.Fatal(err)
	}
	written, err := engine.WriteAuthorizationModel(ctx, writeModelRequest(store.GetId(), model))
	if err != nil {
		t.Fatalf("write model: %v", err)
	}
	modelID := written.GetAuthorizationModelId()

	keys := make([]*openfgav1.TupleKey, 0, len(matrix.Tuples))
	for _, tuple := range matrix.Tuples {
		keys = append(keys, &openfgav1.TupleKey{User: tuple.User, Relation: tuple.Relation, Object: tuple.Object})
	}
	// OpenFGA takes at most 100 tuples per write.
	for batch := range slices.Chunk(keys, 100) {
		if _, err := engine.Write(ctx, &openfgav1.WriteRequest{
			StoreId:              store.GetId(),
			AuthorizationModelId: modelID,
			Writes:               &openfgav1.WriteRequestWrites{TupleKeys: batch},
		}); err != nil {
			t.Fatalf("write tuples: %v", err)
		}
	}

	for _, test := range matrix.Tests {
		t.Run(test.Name, func(t *testing.T) {
			for _, check := range test.Check {
				relations := make([]string, 0, len(check.Assertions))
				for relation := range check.Assertions {
					relations = append(relations, relation)
				}
				slices.Sort(relations)
				for _, relation := range relations {
					want := check.Assertions[relation]
					response, err := engine.Check(ctx, &openfgav1.CheckRequest{
						StoreId:              store.GetId(),
						AuthorizationModelId: modelID,
						TupleKey:             &openfgav1.CheckRequestTupleKey{User: check.User, Relation: relation, Object: check.Object},
					})
					if err != nil {
						t.Errorf("check %s %s on %s: %v", check.User, relation, check.Object, err)
						continue
					}
					if got := response.GetAllowed(); got != want {
						t.Errorf("%s %s on %s = %t, want %t", check.User, relation, check.Object, got, want)
					}
				}
			}
		})
	}
}

// TestAuthorizationModelMatrixCoversEveryPermission fails when the model
// defines a permission the matrix never asserts, so adding one without saying
// who holds it is an incomplete change rather than an untested one.
func TestAuthorizationModelMatrixCoversEveryPermission(t *testing.T) {
	t.Parallel()
	matrix := loadModelTests(t)

	asserted := map[string]bool{}
	for _, test := range matrix.Tests {
		for _, check := range test.Check {
			objectType, _, _ := strings.Cut(check.Object, ":")
			for relation := range check.Assertions {
				asserted[objectType+"#"+relation] = true
			}
		}
	}

	model, err := desiredModel()
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range model.GetTypeDefinitions() {
		for relation := range definition.GetRelations() {
			if !strings.HasPrefix(relation, "can_") {
				continue
			}
			if permission := definition.GetType() + "#" + relation; !asserted[permission] {
				t.Errorf("%s is never asserted in %s; add checks for the roles that hold it and for one that does not", permission, modelTestFile)
			}
		}
	}
}
