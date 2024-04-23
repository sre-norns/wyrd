package dbstore_test

import (
	"testing"

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
	Name string

	Toys []Toy `gorm:"many2many:pet_toys;"`
}

type Pet struct {
	ID   int
	Spec PetSpec `gorm:"embedded"`
}

func TestManyToMany_BUG(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}

	require.NoError(t, db.AutoMigrate(&Pet{}, &Toy{}), "test setup failed DB migration")

	testPet := Pet{
		Spec: PetSpec{Name: "fluffy"},
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
	require.NoError(t, db.Preload("Toys").First(&targetPet, testPet.ID).Error, "fetch prob pet")
	require.Equal(t, 3, len(targetPet.Spec.Toys))
}
