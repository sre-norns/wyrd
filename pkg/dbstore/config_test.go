package dbstore_test

import (
	"testing"

	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/stretchr/testify/require"
)

func launderInt(v int) *int {
	return &v
}

func TestConfig_Dialector(t *testing.T) {
	testCases := map[string]struct {
		given       dbstore.Config
		expectError bool
		expect      string
	}{
		"no URL or DSN is error": {
			expectError: true,
			given:       dbstore.Config{},
		},
		"pgURL": {
			expect: "postgres",
			given: dbstore.Config{
				URL: "postgres://localhost:5432",
			},
		},
		"inmem-is-sqlite": {
			expect: "sqlite",
			given: dbstore.Config{
				URL: "sqlite://:memory:?loc=auto",
			},
		},
		"pgDNS": {
			given: dbstore.Config{
				DSN: "postgres://localhost:5432",
			},
			expectError: true,
		},
		"unsupported_db": {
			given: dbstore.Config{
				URL: "oracle://localhost:5432",
			},
			expectError: true,
		},
		"pgURL+username": {
			expect: "postgres",
			given: dbstore.Config{
				URL:  "postgres://localhost:5432",
				User: "pg",
			},
		},
		"pgURL+username+pass": {
			expect: "postgres",
			given: dbstore.Config{
				URL:      "postgres://localhost:5432",
				User:     "pg",
				Password: "secret",
			},
		},
		"pgURL+port": {
			expect: "postgres",
			given: dbstore.Config{
				URL:  "postgres://localhost:5432",
				Port: launderInt(7777),
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got, err := test.given.Dialector()
			if test.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.expect, got.Name())
			}
		})
	}
}
