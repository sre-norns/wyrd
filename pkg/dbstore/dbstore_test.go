package dbstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Toys []Toy

type ToySpec struct {
	Name string
}

type Toy struct {
	ID   int
	Spec ToySpec `gorm:"embedded"`
}

type PetSpec struct {
	CustomName string

	Toys []Toy `gorm:"many2many:pet_toys;"`
}

type Pet manifest.ResourceModel[PetSpec]

func TestManyToMany_BUG(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer func() {
		dbInstance, _ := db.DB()
		_ = dbInstance.Close()
	}()

	require.NoError(t, db.AutoMigrate(&Pet{}, &Toy{}), "test setup failed DB migration")

	testPet := Pet{
		Spec: PetSpec{CustomName: "fluffy"},
	}
	toys := []Toy{
		{Spec: ToySpec{"toy-1"}},
		{Spec: ToySpec{"toy-2"}},
		{Spec: ToySpec{"toy-3"}},
	}

	require.NoError(t, db.Create(&testPet).Error, "test set-up: creating test pet")
	for _, toy := range toys {
		// Type mismatched: Given &Toy, while association type is '[]Toy'
		require.NoError(t, db.Model(&testPet).Association("Toys").Append(&toy), "adding toys")
	}

	var targetPet Pet
	require.NoError(t, db.Preload("Toys").First(&targetPet, testPet.UID).Error, "fetch prob pet")
	require.Equal(t, 3, len(targetPet.Spec.Toys))
}

func TestNewDBStore(t *testing.T) {
	_, err := dbstore.NewDBStore(nil, dbstore.ManifestModel)
	require.ErrorIs(t, err, dbstore.ErrNoDBObject)
}

func makeTestStore(t *testing.T, given []Pet) (*dbstore.DBStore, func()) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	cleanup := func() {
		dbInstance, _ := db.DB()
		_ = dbInstance.Close()
	}

	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Pet{}, &Toy{}), "test setup failed DB migration")

	store, err := dbstore.NewDBStore(db, dbstore.ManifestModel)
	require.NoError(t, err)

	for _, g := range given {
		require.NoError(t, store.Create(context.TODO(), &g))
	}

	return store, cleanup
}

type petOption func(*Pet)

func withCreatedAt(t time.Time) petOption {
	return func(p *Pet) {
		p.CreatedAt = &t
	}
}

func withDeletedAt(t time.Time) petOption {
	return func(p *Pet) {
		if t.IsZero() {
			p.DeletedAt = nil
		} else {
			p.DeletedAt = &gorm.DeletedAt{
				Time:  t,
				Valid: true,
			}
		}
	}
}

func withVersion(v manifest.Version) petOption {
	return func(p *Pet) {
		p.Version = v
	}
}

func withLabels(labels manifest.Labels) petOption {
	return func(p *Pet) {
		p.Labels = labels
	}
}

func makePet(name, specName string, options ...petOption) Pet {
	p := Pet{
		ObjectMeta: manifest.ObjectMeta{
			Name: manifest.ResourceName(name),
		},
		Spec: PetSpec{
			CustomName: specName,
		},
	}

	for _, o := range options {
		o(&p)
	}

	return p
}

func mockRequirement(t *testing.T, key string, op manifest.Operator, values ...string) manifest.Requirement {
	req, err := manifest.NewRequirement(key, op, values)
	require.NoError(t, err)

	return req
}

func TestDBStore_Find(t *testing.T) {
	testCases := map[string]struct {
		givenQuery manifest.SearchQuery
		given      []Pet
		options    []dbstore.Option

		expectError error
		expectTotal int64
		expect      []Pet
	}{
		"query-all": {
			given: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-2", "some "),
				makePet("pet-3", " value"),
			},
			expectTotal: 3,
			expect: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-2", "some "),
				makePet("pet-3", " value"),
			},
		},

		"query-by_name": {
			given: []Pet{
				makePet("pet-1", "some value"),
				makePet("needle", "some "),
				makePet("pet-3", " value"),
			},
			givenQuery: manifest.SearchQuery{
				Name: "needle",
			},
			expectTotal: 1,
			expect: []Pet{
				makePet("needle", "some "),
			},
		},

		"query-by_partial_name": {
			given: []Pet{
				makePet("pet-1", "some value"),
				makePet("needle", "some "),
				makePet("pet-3", " value"),
			},
			givenQuery: manifest.SearchQuery{
				Name: "need",
			},
			expectTotal: 1,
			expect: []Pet{
				makePet("needle", "some "),
			},
		},

		"query-limited": {
			given: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-2", "some "),
				makePet("pet-3", " value"),
				makePet("pet-4", " other thing"),
			},
			givenQuery: manifest.SearchQuery{
				Limit: 2,
			},

			expectTotal: 4,
			expect: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-2", "some "),
			},
		},
		"query-limited+offset": {
			given: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-2", "some "),
				makePet("pet-3", " value"),
				makePet("pet-4", " other thing"),
			},
			givenQuery: manifest.SearchQuery{
				Offset: 2,
				Limit:  2,
			},

			expectTotal: 4,
			expect: []Pet{
				makePet("pet-3", " value"),
				makePet("pet-4", " other thing"),
			},
		},

		"query-sorted-desc": {
			given: []Pet{
				makePet("pet-1", "some value", withCreatedAt(time.Date(2001, time.August, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-2", "some", withCreatedAt(time.Date(2010, time.September, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-3", "value", withCreatedAt(time.Date(2002, time.November, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-4", "other thing", withCreatedAt(time.Date(2001, time.January, 1, 1, 2, 3, 0, time.Local))),
			},
			options: []dbstore.Option{
				dbstore.OrderByCreatedAt(dbstore.OrderDescending),
			},

			expectTotal: 4,
			expect: []Pet{
				makePet("pet-2", "some"),
				makePet("pet-3", "value"),
				makePet("pet-1", "some value"),
				makePet("pet-4", "other thing"),
			},
		},

		"query-sorted-asc": {
			given: []Pet{
				makePet("pet-1", "some value", withCreatedAt(time.Date(2001, time.August, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-2", "some", withCreatedAt(time.Date(2010, time.September, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-3", "value", withCreatedAt(time.Date(2002, time.November, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-4", "other thing", withCreatedAt(time.Date(2001, time.January, 1, 1, 2, 3, 0, time.Local))),
			},
			options: []dbstore.Option{
				dbstore.OrderByCreatedAt(dbstore.OrderAscending),
			},

			expectTotal: 4,
			expect: []Pet{
				makePet("pet-4", "other thing"),
				makePet("pet-1", "some value"),
				makePet("pet-3", "value"),
				makePet("pet-2", "some"),
			},
		},

		"query-ordered-custom": {
			given: []Pet{
				makePet("unique-name", "gamma"),
				makePet("pet-2", "alpha"),
				makePet("pet-3", "beta"),
			},
			options: []dbstore.Option{
				dbstore.OrderBy("custom_name", dbstore.OrderAscending),
			},
			expectTotal: 3,
			expect: []Pet{
				makePet("pet-2", "alpha"),
				makePet("pet-3", "beta"),
				makePet("unique-name", "gamma"),
			},
		},

		"query-created-after": {
			given: []Pet{
				makePet("pet-1", "some value", withCreatedAt(time.Date(2001, time.August, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-2", "some", withCreatedAt(time.Date(2010, time.September, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-3", "value", withCreatedAt(time.Date(2002, time.November, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-4", "other thing", withCreatedAt(time.Date(2001, time.January, 1, 1, 2, 3, 0, time.Local))),
			},
			options: []dbstore.Option{
				dbstore.OrderByCreatedAt(dbstore.OrderAscending),
			},
			givenQuery: manifest.SearchQuery{
				FromTime: time.Date(2002, time.September, 0, 0, 0, 0, 0, time.Local),
			},
			expectTotal: 2,
			expect: []Pet{
				makePet("pet-3", "value"),
				makePet("pet-2", "some"),
			},
		},
		"query-created-before": {
			given: []Pet{
				makePet("pet-1", "some value", withCreatedAt(time.Date(2001, time.August, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-2", "some", withCreatedAt(time.Date(2010, time.September, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-3", "value", withCreatedAt(time.Date(2002, time.November, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-4", "other thing", withCreatedAt(time.Date(2001, time.January, 1, 1, 2, 3, 0, time.Local))),
			},
			options: []dbstore.Option{
				dbstore.OrderByCreatedAt(dbstore.OrderAscending),
			},
			givenQuery: manifest.SearchQuery{
				TillTime: time.Date(2002, time.November, 0, 0, 0, 0, 0, time.Local),
			},
			expectTotal: 2,
			expect: []Pet{
				makePet("pet-4", "other thing"),
				makePet("pet-1", "some value"),
			},
		},

		"query-created-between": {
			given: []Pet{
				makePet("pet-1", "some value", withCreatedAt(time.Date(2001, time.August, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-2", "some", withCreatedAt(time.Date(2010, time.September, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-3", "value", withCreatedAt(time.Date(2002, time.November, 1, 1, 2, 3, 0, time.Local))),
				makePet("pet-4", "other thing", withCreatedAt(time.Date(2001, time.January, 1, 1, 2, 3, 0, time.Local))),
			},
			options: []dbstore.Option{
				dbstore.OrderByCreatedAt(dbstore.OrderAscending),
			},
			givenQuery: manifest.SearchQuery{
				FromTime: time.Date(2001, time.January, 0, 0, 0, 0, 0, time.Local),
				TillTime: time.Date(2003, time.January, 0, 0, 0, 0, 0, time.Local),
			},
			expectTotal: 3,
			expect: []Pet{
				makePet("pet-4", "other thing"),
				makePet("pet-1", "some value"),
				makePet("pet-3", "value"),
			},
		},

		"query-all-soft-deleted": {
			given: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-2", "some "),
				makePet("pet-3", "expired", withDeletedAt(time.Now())),
				makePet("pet-4", "other"),
			},
			options:     []dbstore.Option{},
			expectTotal: 3,
			expect: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-2", "some "),
				makePet("pet-4", "other"),
			},
		},

		"query-all-unscoped": {
			given: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-2", "some "),
				makePet("pet-3", "expired", withDeletedAt(time.Now())),
				makePet("pet-4", "other"),
			},
			options: []dbstore.Option{
				dbstore.IncludeDeleted(),
			},
			expectTotal: 4,
			expect: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-2", "some "),
				makePet("pet-3", "expired"),
				makePet("pet-4", "other"),
			},
		},

		"query-all-non-counting": {
			given: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-2", "some "),
				makePet("pet-3", " value"),
			},
			options: []dbstore.Option{
				dbstore.Count(false),
			},
			expectTotal: 0,
			expect: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-2", "some "),
				makePet("pet-3", " value"),
			},
		},

		// The use of WithVersion option makes no sense for .Find, but in case someone wants to try it:
		"query-with-version?": {
			given: []Pet{
				makePet("pet-1", "some value", withVersion(19)),
				makePet("pet-2", "some", withVersion(2)),
				makePet("pet-3", "value"),
				makePet("pet-4", "another", withVersion(2)),
			},
			options: []dbstore.Option{
				dbstore.WithVersion(3),
			},
			expectTotal: 2,
			expect: []Pet{
				makePet("pet-2", "some"),
				makePet("pet-4", "another"),
			},
		},
		// The use of WithVersion option makes no sense for .Find, but in case someone wants to try it:
		"query-using-labels": {
			given: []Pet{
				makePet("pet-1", "some value", withLabels(manifest.Labels{"label1": "", "env": "xyz", "size": "128"})),
				makePet("pet-2", "some", withLabels(manifest.Labels{"label2": "", "env": "not", "size": "32"})),
				makePet("pet-3", "value", withLabels(manifest.Labels{"label3": "", "size": "128"})),
				makePet("pet-4", "another", withLabels(manifest.Labels{"label4": "", "env": "xyz", "size": "0"})),
			},
			givenQuery: manifest.SearchQuery{
				Selector: manifest.NewSelector(
					mockRequirement(t, "env", manifest.Equals, "xyz"),
				),
			},
			expectTotal: 2,
			expect: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-4", "another"),
			},
		},
		"query-using-not_in": {
			given: []Pet{
				makePet("pet-1", "some value", withLabels(manifest.Labels{"label1": "", "env": "xyz", "size": "128"})),
				makePet("pet-2", "some", withLabels(manifest.Labels{"label2": "", "env": "not", "size": "32"})),
				makePet("pet-3", "value", withLabels(manifest.Labels{"label3": "", "size": "128"})),
				makePet("pet-4", "another", withLabels(manifest.Labels{"label4": "", "env": "xyz", "size": "0"})),
				makePet("pet-5", "another", withLabels(manifest.Labels{"special": "", "label": "not-this", "env": "unnatural", "size": "-10"})),
				makePet("pet-6", "another", withLabels(manifest.Labels{"special": "", "label": "maybe-this", "env": "xyz", "size": "-10"})), // This
				makePet("pet-6.2", "another", withLabels(manifest.Labels{"special": "", "label": "maybe-this", "env": "xyz", "size": "-256"})),
				makePet("pet-7", "another", withLabels(manifest.Labels{"special": "", "label": "you-in-particular", "env": "natural", "size": "-10"})),
				makePet("pet-8", "another", withLabels(manifest.Labels{"special": "", "label": "", "env": "natural", "size": "-32"})), // This
				makePet("pet-9", "another", withLabels(manifest.Labels{"special": "", "common": "also", "env": "xyz", "size": "-127"})),
				makePet("unnamed", "another", withLabels(manifest.Labels{"special": "", "env": "natural", "size": "-32"})),
			},
			givenQuery: manifest.SearchQuery{
				Name: "pet",
				Selector: manifest.NewSelector(
					mockRequirement(t, "env", manifest.NotIn, "xyz", "natural"),
				),
			},
			expectTotal: 2,
			expect: []Pet{
				makePet("pet-2", "some"),
				makePet("pet-5", "another"),
			},
		},

		"query-using-all-labels": {
			given: []Pet{
				makePet("pet-1", "some value", withLabels(manifest.Labels{"label1": "", "env": "xyz", "size": "128"})),
				makePet("pet-2", "some", withLabels(manifest.Labels{"label2": "", "env": "not", "size": "32"})),
				makePet("pet-3", "value", withLabels(manifest.Labels{"label3": "", "size": "128"})),
				makePet("pet-4", "another", withLabels(manifest.Labels{"label4": "", "env": "xyz", "size": "0"})),
				makePet("pet-5", "another", withLabels(manifest.Labels{"special": "", "label": "not-this", "env": "unnatural", "size": "-10"})),
				makePet("pet-6", "another", withLabels(manifest.Labels{"special": "", "label": "maybe-this", "env": "xyz", "size": "-10"})), // This
				makePet("pet-6.2", "another", withLabels(manifest.Labels{"special": "", "label": "maybe-this", "env": "xyz", "size": "-256"})),
				makePet("pet-7", "another", withLabels(manifest.Labels{"special": "", "label": "you-in-particular", "env": "natural", "size": "-10"})),
				makePet("pet-8", "another", withLabels(manifest.Labels{"special": "", "label": "", "env": "natural", "size": "-32"})), // This
				makePet("pet-9", "another", withLabels(manifest.Labels{"special": "", "common": "also", "env": "xyz", "size": "-127"})),
				makePet("unnamed", "another", withLabels(manifest.Labels{"special": "", "env": "natural", "size": "-32"})),
			},
			givenQuery: manifest.SearchQuery{
				Name: "pet",
				Selector: manifest.NewSelector(
					mockRequirement(t, "special", manifest.Exists),
					mockRequirement(t, "common", manifest.DoesNotExist),
					mockRequirement(t, "env", manifest.In, "xyz", "natural"),
					mockRequirement(t, "label", manifest.NotEquals, "you-in-particular"),
					mockRequirement(t, "size", manifest.GreaterThan, "-128"),
					mockRequirement(t, "size", manifest.LessThan, "-3"),
				),
			},
			expectTotal: 2,
			expect: []Pet{
				makePet("pet-6", "another"),
				makePet("pet-8", "another"),
			},
		},

		"invalid-requirement-0": {
			given: []Pet{
				makePet("pet-1", "some value", withLabels(manifest.Labels{"label1": "", "env": "xyz", "size": "128"})),
			},
			givenQuery: manifest.SearchQuery{
				Selector: manifest.NewSelector(
					mockRequirement(t, "env", manifest.Equals),
				),
			},
			expectError: dbstore.ErrNoRequirementsValueProvided,
		},
		"invalid-requirement-1": {
			given: []Pet{
				makePet("pet-1", "some value", withLabels(manifest.Labels{"label1": "", "env": "xyz", "size": "128"})),
			},
			givenQuery: manifest.SearchQuery{
				Selector: manifest.NewSelector(
					mockRequirement(t, "env", manifest.NotEquals),
				),
			},
			expectError: dbstore.ErrNoRequirementsValueProvided,
		},
		"invalid-requirement-2": {
			given: []Pet{
				makePet("pet-1", "some value", withLabels(manifest.Labels{"label1": "", "env": "xyz", "size": "128"})),
			},
			givenQuery: manifest.SearchQuery{
				Selector: manifest.NewSelector(
					mockRequirement(t, "size", manifest.GreaterThan, "cloud"),
				),
			},
			expectError: manifest.ErrNonSelectableRequirements,
		},
		"invalid-requirement-2-0": {
			given: []Pet{
				makePet("pet-1", "some value", withLabels(manifest.Labels{"label1": "", "env": "xyz", "size": "128"})),
			},
			givenQuery: manifest.SearchQuery{
				Selector: manifest.NewSelector(
					mockRequirement(t, "size", manifest.GreaterThan),
				),
			},
			expectError: dbstore.ErrNoRequirementsValueProvided,
		},
		"invalid-requirement-3": {
			given: []Pet{
				makePet("pet-1", "some value", withLabels(manifest.Labels{"label1": "", "env": "xyz", "size": "128"})),
			},
			givenQuery: manifest.SearchQuery{
				Selector: manifest.NewSelector(
					mockRequirement(t, "size", manifest.LessThan, "cloud"),
				),
			},
			expectError: manifest.ErrNonSelectableRequirements,
		},
		"invalid-requirement-3-0": {
			given: []Pet{
				makePet("pet-1", "some value", withLabels(manifest.Labels{"label1": "", "env": "xyz", "size": "128"})),
			},
			givenQuery: manifest.SearchQuery{
				Selector: manifest.NewSelector(
					mockRequirement(t, "size", manifest.LessThan),
				),
			},
			expectError: dbstore.ErrNoRequirementsValueProvided,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			store, cleanup := makeTestStore(t, test.given)
			defer cleanup()

			var got []Pet
			total, err := store.Find(context.TODO(), &got, test.givenQuery, test.options...)
			if test.expectError != nil {
				require.ErrorIs(t, err, test.expectError)
			} else {
				require.NoError(t, err)

				filtered := make([]Pet, 0, len(got))
				for _, g := range got {
					filtered = append(filtered, Pet{
						ObjectMeta: manifest.ObjectMeta{Name: g.Name},
						Spec:       g.Spec,
					})
				}
				require.Equal(t, test.expect, filtered)
				require.Equal(t, test.expectTotal, total)
			}
		})
	}
}

func TestDBStore_FindNames(t *testing.T) {
	testCases := map[string]struct {
		given      []Pet
		model      any
		givenQuery manifest.SearchQuery
		options    []dbstore.Option

		expectError error
		expect      manifest.StringSet
	}{
		"query-all": {
			model: &Pet{},
			given: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-2", "some "),
				makePet("pet-3", " value"),
			},
			expect: manifest.StringSet{
				"pet-1": struct{}{},
				"pet-2": struct{}{},
				"pet-3": struct{}{},
			},
		},
		"query-by_name": {
			model: &Pet{},
			given: []Pet{
				makePet("unique-name", "some value"),
				makePet("pet-2", "some "),
				makePet("pet-3", " value"),
			},
			givenQuery: manifest.SearchQuery{
				Name: "pet",
			},
			expect: manifest.StringSet{
				"pet-2": struct{}{},
				"pet-3": struct{}{},
			},
		},
		"query-by_labels": {
			model: &Pet{},
			given: []Pet{
				makePet("unique-name", "some value", withLabels(manifest.Labels{"label1": "", "env": "xyz", "size": "128"})),
				makePet("pet-2", "some ", withLabels(manifest.Labels{"label1": "", "env": "nature", "size": "128"})),
				makePet("pet-3", " value", withLabels(manifest.Labels{"label1": "", "env": "other", "size": "128"})),
			},
			givenQuery: manifest.SearchQuery{
				Selector: manifest.NewSelector(
					mockRequirement(t, "env", manifest.In, "nature", "xyz"),
				),
			},
			expect: manifest.StringSet{
				"unique-name": struct{}{},
				"pet-2":       struct{}{},
			},
		},
		"query-ordered": {
			model: &Pet{},
			given: []Pet{
				makePet("unique-name", "some value", withLabels(manifest.Labels{"label1": "", "env": "xyz", "size": "128"})),
				makePet("pet-2", "some ", withLabels(manifest.Labels{"label1": "", "env": "nature", "size": "128"})),
				makePet("pet-3", " value", withLabels(manifest.Labels{"label1": "", "env": "other", "size": "128"})),
			},
			givenQuery: manifest.SearchQuery{
				Selector: manifest.NewSelector(
					mockRequirement(t, "env", manifest.In, "nature", "xyz"),
				),
			},
			options: []dbstore.Option{
				dbstore.OrderByCreatedAt(dbstore.OrderDescending),
			},
			expect: manifest.StringSet{
				"pet-2":       struct{}{},
				"unique-name": struct{}{},
			},
		},

		"query-invalid_selector": {
			model: &Pet{},
			given: []Pet{
				makePet("unique-name", "some value", withLabels(manifest.Labels{"label1": "", "env": "xyz", "size": "128"})),
				makePet("pet-2", "some ", withLabels(manifest.Labels{"label1": "", "env": "nature", "size": "128"})),
				makePet("pet-3", " value", withLabels(manifest.Labels{"label1": "", "env": "other", "size": "128"})),
			},
			givenQuery: manifest.SearchQuery{
				Selector: manifest.NewSelector(
					mockRequirement(t, "env", manifest.Equals),
				),
			},
			expectError: dbstore.ErrNoRequirementsValueProvided,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			store, cleanup := makeTestStore(t, test.given)
			defer cleanup()

			got, err := store.FindNames(context.TODO(), test.model, test.givenQuery, test.options...)
			if test.expectError != nil {
				require.ErrorIs(t, err, test.expectError)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.expect, got)
			}
		})
	}
}

func TestDBStore_FindLabels(t *testing.T) {
	testCases := map[string]struct {
		given      []Pet
		model      any
		givenQuery manifest.SearchQuery
		options    []dbstore.Option

		expectError error
		expect      manifest.StringSet
	}{
		"query-all": {
			model: &Pet{},
			given: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-2", "some ", withLabels(manifest.Labels{"label1": "", "env": "nature", "size": "128"})),
				makePet("pet-3", " value", withLabels(manifest.Labels{"label2": "", "env": "xyz", "size": "128"})),
			},
			expect: manifest.StringSet{
				"label1": struct{}{},
				"label2": struct{}{},
				"env":    struct{}{},
				"size":   struct{}{},
			},
		},
		"query-by_name": {
			model: &Pet{},
			given: []Pet{
				makePet("unique-name", "some value"),
				makePet("pet-2", "some ", withLabels(manifest.Labels{"label1": "", "env": "nature", "size": "128"})),
				makePet("pet-3", " value", withLabels(manifest.Labels{"label2": "", "env": "xyz", "size": "128"})),
				makePet("pet-4", "some ", withLabels(manifest.Labels{"label3": "", "env": "nature", "mize": "128"})),
				makePet("pet-5", " value", withLabels(manifest.Labels{"label4": "", "environment": "xyz", "width": "128"})),
			},
			givenQuery: manifest.SearchQuery{
				Name: "env",
			},
			expect: manifest.StringSet{
				"env":         struct{}{},
				"environment": struct{}{},
			},
		},
		"query-by_labels-makes-no-sense": {
			model: &Pet{},
			given: []Pet{
				makePet("unique-name", "some value"),
				makePet("pet-2", "some ", withLabels(manifest.Labels{"label": "", "env": "nature", "size": "128"})),
				makePet("pet-3", " value", withLabels(manifest.Labels{"label": "", "env": "xyz", "size": "128"})),
				makePet("pet-4", "some ", withLabels(manifest.Labels{"label": "", "env": "nature", "mize": "128"})),
				makePet("pet-5", " value", withLabels(manifest.Labels{"label": "", "environment": "xyz", "size": "128"})),
			},
			givenQuery: manifest.SearchQuery{
				Selector: manifest.NewSelector(
					mockRequirement(t, "env", manifest.In, "nature", "xyz"),
					mockRequirement(t, "environment", manifest.DoesNotExist),
				),
			},
			expect: manifest.StringSet{
				"label":       struct{}{},
				"env":         struct{}{},
				"size":        struct{}{},
				"environment": struct{}{},
				"mize":        struct{}{},
			},
		},
		"query-deleted": {
			model: &Pet{},
			given: []Pet{
				makePet("unique-name", "some value", withLabels(manifest.Labels{"label1": "", "env": "xyz", "size": "128"})),
				makePet("pet-2", "some ", withLabels(manifest.Labels{"unique1": "", "env": "nature", "size": "128"}), withDeletedAt(time.Now())),
				makePet("pet-3", " value", withLabels(manifest.Labels{"label1": "", "env": "other", "size": "128"})),

				makePet("pet-3", " value", withLabels(manifest.Labels{"label1": "", "env": "other", "size": "128"})),
				makePet("pet-3", " value", withLabels(manifest.Labels{"label1": "", "unique2": "128"}), withDeletedAt(time.Now())),
			},
			options: []dbstore.Option{
				dbstore.IncludeDeleted(),
			},
			expect: manifest.StringSet{
				"label1":  struct{}{},
				"env":     struct{}{},
				"size":    struct{}{},
				"unique1": struct{}{},
				"unique2": struct{}{},
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			store, cleanup := makeTestStore(t, test.given)
			defer cleanup()

			got, err := store.FindLabels(context.TODO(), test.model, test.givenQuery, test.options...)
			if test.expectError != nil {
				require.ErrorIs(t, err, test.expectError)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.expect, got)
			}
		})
	}
}

func TestDBStore_FindLabelValues(t *testing.T) {
	testCases := map[string]struct {
		store      []Pet
		model      any
		givenQuery manifest.SearchQuery
		given      string
		options    []dbstore.Option

		expectError error
		expect      manifest.StringSet
	}{
		"query-all": {
			model: &Pet{},
			store: []Pet{
				makePet("pet-1", "some value"),
				makePet("pet-2", "some ", withLabels(manifest.Labels{"label1": "", "env": "nature", "size": "128"})),
				makePet("pet-3", " value", withLabels(manifest.Labels{"label2": "", "env": "xyz", "size": "128"})),
			},
			given: "env",
			expect: manifest.StringSet{
				"nature": struct{}{},
				"xyz":    struct{}{},
			},
		},
		"query-by_name": {
			model: &Pet{},
			store: []Pet{
				makePet("unique-name", "some value"),
				makePet("pet-2", "some ", withLabels(manifest.Labels{"label1": "", "env": "nature", "size": "128"})),
				makePet("pet-3", " value", withLabels(manifest.Labels{"label2": "", "env": "xyz", "size": "128"})),
				makePet("pet-4", "some ", withLabels(manifest.Labels{"label3": "", "env": "natural", "mize": "128"})),
				makePet("pet-5", " value", withLabels(manifest.Labels{"label4": "", "environment": "xyz", "width": "128"})),
				makePet("pet-6", "some ", withLabels(manifest.Labels{"label3": "", "env": "unnatural", "mize": "128"})),
			},
			givenQuery: manifest.SearchQuery{
				Name: "nat",
			},
			given: "env",
			expect: manifest.StringSet{
				"nature":    struct{}{},
				"natural":   struct{}{},
				"unnatural": struct{}{},
			},
		},
		"query-by_labels-makes-no-sense": {
			model: &Pet{},
			store: []Pet{
				makePet("unique-name", "some value"),
				makePet("pet-2", "some ", withLabels(manifest.Labels{"label": "", "env": "nature", "size": "128"})),
				makePet("pet-3", " value", withLabels(manifest.Labels{"label": "", "env": "xyz", "size": "128"})),
				makePet("pet-4", "some ", withLabels(manifest.Labels{"label": "", "env": "nature", "mize": "128"})),
				makePet("pet-5", " value", withLabels(manifest.Labels{"label": "", "environment": "xyz", "size": "128"})),
			},
			givenQuery: manifest.SearchQuery{
				Selector: manifest.NewSelector(
					mockRequirement(t, "env", manifest.In, "nature", "xyz"),
					mockRequirement(t, "environment", manifest.DoesNotExist),
				),
			},
			given: "env",
			expect: manifest.StringSet{
				"xyz":    struct{}{},
				"nature": struct{}{},
			},
		},
		"query-deleted": {
			model: &Pet{},
			store: []Pet{
				makePet("unique-name", "some value", withLabels(manifest.Labels{"label1": "", "env": "xyz", "size": "128"})),
				makePet("pet-2", "some ", withLabels(manifest.Labels{"unique1": "", "size": "128"}), withDeletedAt(time.Now())),
				makePet("pet-3", " value", withLabels(manifest.Labels{"label1": "", "env": "other", "size": "128"})),

				makePet("pet-3", " value", withLabels(manifest.Labels{"label1": "", "env": "other", "size": "128"})),
				makePet("pet-3", " value", withLabels(manifest.Labels{"label1": "", "env": "knowhere", "unique2": "128"}), withDeletedAt(time.Now())),
			},
			options: []dbstore.Option{
				dbstore.IncludeDeleted(),
			},
			given: "env",
			expect: manifest.StringSet{
				"xyz":      struct{}{},
				"other":    struct{}{},
				"knowhere": struct{}{},
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			store, cleanup := makeTestStore(t, test.store)
			defer cleanup()

			got, err := store.FindLabelValues(context.TODO(), test.model, test.given, test.givenQuery, test.options...)
			if test.expectError != nil {
				require.ErrorIs(t, err, test.expectError)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.expect, got)
			}
		})
	}
}
