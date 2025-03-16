package dbstore_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	values []manifest.Model

	sideEffectSearchQuery manifest.SearchQuery
}

func assign(dest any, value manifest.Model) error {
	destType := reflect.TypeOf(dest)
	if destType.Kind() != reflect.Pointer {
		return errors.New("dest is not a pointer")
	}

	valueType := reflect.TypeOf(value)
	if !valueType.AssignableTo(destType.Elem()) {
		return errors.New("value not assignable to dest")
	}

	d := dest.(*manifest.Model)
	*d = value

	return nil
}

func assignSlice(dest any, value []manifest.Model) error {
	destType := reflect.TypeOf(dest)
	if destType.Kind() != reflect.Pointer {
		return errors.New("dest is not a pointer")
	}

	valueType := reflect.TypeOf(value)
	if !valueType.AssignableTo(destType.Elem()) {
		return fmt.Errorf("value of type %v not assignable to dest of type %v", valueType, destType)
	}

	d := dest.(*[]manifest.Model)
	*d = value

	return nil
}

func (f *fakeStore) GetByUID(ctx context.Context, dest any, id manifest.ResourceID, options ...dbstore.Option) (exists bool, err error) {
	for _, v := range f.values {
		if v.GetMetadata().UID == id {
			return true, assign(dest, v)
		}
	}

	return false, nil
}

func (f *fakeStore) GetByName(ctx context.Context, dest any, id manifest.ResourceName, options ...dbstore.Option) (exists bool, err error) {
	for _, v := range f.values {
		if v.GetMetadata().Name == id {
			return true, assign(dest, v)
		}
	}

	return false, nil
}

var errNotImplemented = fmt.Errorf("not implemented")

func (f *fakeStore) Create(ctx context.Context, value any, options ...dbstore.Option) error {
	return errNotImplemented
}

func (f *fakeStore) CreateOrUpdate(ctx context.Context, newValue any, options ...dbstore.Option) (exists bool, err error) {
	return false, errNotImplemented
}

func (f *fakeStore) Update(ctx context.Context, newValue any, id manifest.ResourceID, options ...dbstore.Option) (exists bool, err error) {
	return false, errNotImplemented
}

func (f *fakeStore) Delete(ctx context.Context, model any, id manifest.ResourceID, version manifest.Version, options ...dbstore.Option) (existed bool, err error) {
	return false, errNotImplemented
}

func (f *fakeStore) Restore(ctx context.Context, model any, id manifest.ResourceID, options ...dbstore.Option) (existed bool, err error) {
	return false, errNotImplemented
}

func (f *fakeStore) Find(ctx context.Context, dest any, searchQuery manifest.SearchQuery, options ...dbstore.Option) (total int64, err error) {
	// This is a special handling by mock-store to emulate real store connection issues
	if f.values == nil {
		return 0, fmt.Errorf("store error")
	}

	if searchQuery.Offset > uint(len(f.values)) {
		return int64(len(f.values)), fmt.Errorf("offset %d outside of the range [%d; %d)", searchQuery.Offset, 0, len(f.values))
	}

	if searchQuery.Limit == 0 {
		searchQuery.Limit = uint(len(f.values)) - searchQuery.Offset
	}

	end := min(searchQuery.Offset+searchQuery.Limit, uint(len(f.values)))

	f.sideEffectSearchQuery = searchQuery
	results := f.values[searchQuery.Offset:end]

	return int64(len(f.values)), assignSlice(dest, results)
}

func mockStore(values []manifest.Model) dbstore.Store {
	return &fakeStore{
		values: values,
	}
}

func makeMockModel(value string) manifest.Model {
	return &MockModel{value: value}
}

func makeMeta(name string) manifest.ObjectMeta {
	return manifest.ObjectMeta{
		Name: manifest.ResourceName(name),
	}
}

func Test_ForEach(t *testing.T) {
	testCases := map[string]struct {
		ctx         context.Context
		query       manifest.SearchQuery
		given       []manifest.Model
		expectCount int64
		expectError bool
		expect      []manifest.ObjectMeta
	}{
		"base_case": {
			ctx:   context.Background(),
			query: manifest.SearchQuery{},
			given: []manifest.Model{
				makeMockModel("test"),
			},
			expectCount: 1,
			expect: []manifest.ObjectMeta{
				makeMeta("test"),
			},
		},
		"store_disconnected": {
			ctx:         context.Background(),
			given:       nil,
			expectError: true,
			expectCount: 0,
		},
		"mapper_error_stops_loop": {
			ctx:   context.Background(),
			query: manifest.SearchQuery{},
			given: []manifest.Model{
				makeMockModel("test-0"),
				makeMockModel("test-1"),
				makeMockModel("test-2"),
				makeMockModel("<POISON>"),
				makeMockModel("test-4"),
			},
			expectCount: 3,
			expectError: true,
		},
		"cancel_context_stops_loop": {
			ctx:   context.Background(),
			query: manifest.SearchQuery{},
			given: []manifest.Model{
				makeMockModel("test-0"),
				makeMockModel("test-1"),
				makeMockModel("test-2"),
				makeMockModel("<TIMEOUT>"),
				makeMockModel("test-4"),
			},
			expectCount: 5,
			expectError: false,
			expect: []manifest.ObjectMeta{
				makeMeta("test-0"),
				makeMeta("test-1"),
				makeMeta("test-2"),
				makeMeta("<TIMEOUT>"),
				makeMeta("test-4"),
			},
		},
		"cancel_context_stops_next_page": {
			ctx: context.Background(),
			query: manifest.SearchQuery{
				Limit: 3,
			},
			given: []manifest.Model{
				makeMockModel("test-0"),
				makeMockModel("<TIMEOUT>"),
				makeMockModel("test-1"),
				makeMockModel("test-2"),
				makeMockModel("test-4"),
			},
			expectCount: 3,
			expectError: false,
			expect: []manifest.ObjectMeta{
				makeMeta("test-0"),
				makeMeta("<TIMEOUT>"),
				makeMeta("test-1"),
			},
		},

		"paginated_query": {
			ctx: context.Background(),
			query: manifest.SearchQuery{
				Limit: 3,
			},
			given: []manifest.Model{
				makeMockModel("test-0"),
				makeMockModel("test-1"),
				makeMockModel("test-2"),
				makeMockModel("test-3"),
				makeMockModel("test-4"),
			},
			expectCount: 5,
			expect: []manifest.ObjectMeta{
				makeMeta("test-0"),
				makeMeta("test-1"),
				makeMeta("test-2"),
				makeMeta("test-3"),
				makeMeta("test-4"),
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			testCtx, cancel := context.WithCancel(test.ctx)
			sideEffectStore := mockStore(test.given)
			got := make([]manifest.ObjectMeta, 0, len(test.expect))
			gotCount, gotErr := dbstore.ForEach(testCtx, sideEffectStore, test.query, func(m manifest.Model) error {
				got = append(got, m.GetMetadata())
				switch m.GetMetadata().Name {
				case "<POISON>":
					return fmt.Errorf("stopping the mapper")
				case "<TIMEOUT>":
					cancel()
				}
				return nil
			})

			if test.expectError {
				require.Error(t, gotErr)
			} else {
				require.NoError(t, gotErr)
				require.Equal(t, test.expect, got)
			}

			require.Equal(t, test.expectCount, gotCount)
		})
	}
}

type MockModel struct {
	value string
}

func (m *MockModel) GetTypeMetadata() manifest.TypeMeta {
	return manifest.TypeMeta{
		Kind: manifest.Kind("mock"),
	}
}

func (m *MockModel) GetMetadata() manifest.ObjectMeta {
	return manifest.ObjectMeta{
		Name: manifest.ResourceName(m.value),
	}
}

func (m *MockModel) GetSpec() any {
	return m.value
}

func (m *MockModel) GetStatus() any {
	return nil
}

func ExampleForEach() {
	models := []manifest.Model{&MockModel{value: "value"}, &MockModel{value: "value2"}}
	store := mockStore(models)

	processed, _ := dbstore.ForEach(context.TODO(), store, manifest.SearchQuery{}, func(m manifest.Model) error {
		fmt.Printf("-item: %v\n", m.GetSpec())
		return nil
	})

	fmt.Printf("total: %d\n", processed)
	// Output:
	// -item: value
	// -item: value2
	// total: 2
}
