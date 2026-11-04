package dbstore_test

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sre-norns/wyrd/internal/pgtest"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	// leakSecret holds every character that breaks an unencoded URL userinfo.
	leakSecret = "Zq9/Xk#Pw?Lm@Vb%:Ty"
	// leakUser is a distinctive user name: the user part of a URL may be a token.
	leakUser = "uK7tok"
)

// requireNoSecret fails when err, in any rendering or by unwrapping, exposes a
// secret or any 3-character substring of one.
func requireNoSecret(t *testing.T, err error, secrets ...string) {
	t.Helper()
	if err == nil {
		return
	}

	for _, text := range []string{err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
		for _, secret := range secrets {
			for i := 0; i+3 <= len(secret); i++ {
				require.NotContains(t, text, secret[i:i+3], "error exposes part of a secret")
			}
		}
	}
	require.False(t, errors.As(err, new(*url.Error)), "error wraps a *url.Error, which quotes its input")
	require.False(t, errors.As(err, new(*pgconn.ParseConfigError)), "error wraps a pgx parse error, which holds the connection string")
}

// openDialector runs Dialector and, when it succeeds, gorm.Open without a
// ping, so that no network access is needed. It returns the first error.
func openDialector(c dbstore.Config) (gorm.Dialector, error) {
	d, err := c.Dialector()
	if err != nil {
		return nil, err
	}
	_, err = gorm.Open(d, &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	return d, err
}

// postgresDSN returns the DSN of a Postgres Dialector.
func postgresDSN(t *testing.T, d gorm.Dialector) string {
	t.Helper()
	pg, ok := d.(*postgres.Dialector)
	require.True(t, ok, "expected a Postgres dialector, got %T", d)
	return pg.Config.DSN
}

func TestConfig_DialectorNeverLeaksCredentials(t *testing.T) {
	testCases := map[string]struct {
		given      dbstore.Config
		secrets    []string
		invalidURL bool // errors.Is(err, ErrInvalidDBUrl) must hold
		wantErr    bool
	}{
		"postgres URL, password with /": {
			given:      dbstore.Config{URL: "postgres://" + leakUser + ":Zq9X/k7PwLm@localhost/db"},
			secrets:    []string{leakUser, "Zq9X/k7PwLm"},
			invalidURL: true,
		},
		"postgres URL, password with #": {
			given:      dbstore.Config{URL: "postgres://" + leakUser + ":Zq9X#k7PwLm@localhost/db"},
			secrets:    []string{leakUser, "Zq9X#k7PwLm"},
			invalidURL: true,
		},
		"postgres URL, password with ?": {
			given:      dbstore.Config{URL: "postgres://" + leakUser + ":Zq9X?k7PwLm@localhost/db"},
			secrets:    []string{leakUser, "Zq9X?k7PwLm"},
			invalidURL: true,
		},
		// net/url accepts these in userinfo; they must still never surface.
		"postgres URL, password with @": {
			given:   dbstore.Config{URL: "postgres://" + leakUser + ":Zq9X@k7PwLm@localhost/db"},
			secrets: []string{leakUser, "Zq9X@k7PwLm"},
		},
		"postgres URL, password with :": {
			given:   dbstore.Config{URL: "postgres://" + leakUser + ":Zq9X:k7PwLm@localhost/db"},
			secrets: []string{leakUser, "Zq9X:k7PwLm"},
		},
		"postgres URL, full secret": {
			given:      dbstore.Config{URL: "postgres://" + leakUser + ":" + leakSecret + "@localhost/db"},
			secrets:    []string{leakUser, leakSecret},
			invalidURL: true,
		},
		"postgresql URL, full secret": {
			given:      dbstore.Config{URL: "postgresql://" + leakUser + ":" + leakSecret + "@localhost/db?sslmode=disable"},
			secrets:    []string{leakUser, leakSecret},
			invalidURL: true,
		},
		"mysql URL, full secret": {
			given:      dbstore.Config{URL: "mysql://" + leakUser + ":" + leakSecret + "@localhost/db"},
			secrets:    []string{leakUser, leakSecret},
			invalidURL: true,
		},
		"sqlite URL, full secret": {
			given:      dbstore.Config{URL: "sqlite://" + leakUser + ":" + leakSecret + "@/tmp/db.sqlite"},
			secrets:    []string{leakUser, leakSecret},
			invalidURL: true,
		},
		"unsupported scheme with credentials": {
			given:   dbstore.Config{URL: "oracle://" + leakUser + ":Zq9Xk7PwLm@localhost/db"},
			secrets: []string{leakUser, "Zq9Xk7PwLm"},
			wantErr: true,
		},
		"unknown scheme with credentials": {
			given:      dbstore.Config{URL: "nosuchdb://" + leakUser + ":Zq9Xk7PwLm@localhost/db"},
			secrets:    []string{leakUser, "Zq9Xk7PwLm"},
			invalidURL: true,
		},
		"postgres URL with invalid query option": {
			given:   dbstore.Config{URL: "postgres://" + leakUser + ":Zq9Xk7PwLm@localhost/db?port=notaport"},
			secrets: []string{leakUser, "Zq9Xk7PwLm"},
			wantErr: true,
		},
		"postgres URL with DSN": {
			given:   dbstore.Config{URL: "postgres://localhost/db", DSN: "password=Zq9Xk7PwLm"},
			secrets: []string{"Zq9Xk7PwLm"},
			wantErr: true,
		},
		"postgres URL plus Password override": {
			given:   dbstore.Config{URL: "postgres://127.0.0.1:5432/db", User: leakUser, Password: leakSecret},
			secrets: []string{leakSecret},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := openDialector(tc.given)
			requireNoSecret(t, err, tc.secrets...)
			if tc.invalidURL {
				require.ErrorIs(t, err, dbstore.ErrInvalidDBUrl)
			}
			if tc.wantErr {
				require.Error(t, err)
			}
		})
	}
}

func TestConfig_DialectorPostgresOverrides(t *testing.T) {
	const awkward = `p a's\w="/#?@:%20+&x`

	testCases := map[string]struct {
		given        dbstore.Config
		wantUser     string
		wantPassword string
		wantDatabase string
		wantHost     string
		wantPort     uint16
		wantRuntime  map[string]string
	}{
		"no overrides": {
			given:        dbstore.Config{URL: "postgres://svc:pw@db.internal:6432/app?sslmode=disable"},
			wantUser:     "svc",
			wantPassword: "pw",
			wantDatabase: "app",
			wantHost:     "db.internal",
			wantPort:     6432,
		},
		"user and password with reserved characters": {
			given:        dbstore.Config{URL: "postgres://127.0.0.1:5432/db", User: `o'neil \ admin=1`, Password: awkward},
			wantUser:     `o'neil \ admin=1`,
			wantPassword: awkward,
			wantDatabase: "db",
			wantHost:     "127.0.0.1",
			wantPort:     5432,
		},
		"password override keeps URL user": {
			given:        dbstore.Config{URL: "postgres://svc:old@localhost:5432/db", Password: awkward},
			wantUser:     "svc",
			wantPassword: awkward,
			wantDatabase: "db",
			wantHost:     "localhost",
			wantPort:     5432,
		},
		"user override keeps URL password": {
			given:        dbstore.Config{URL: "postgres://svc:keep@localhost:5432/db", User: "other"},
			wantUser:     "other",
			wantPassword: "keep",
			wantDatabase: "db",
			wantHost:     "localhost",
			wantPort:     5432,
		},
		"dbname override": {
			given:        dbstore.Config{URL: "postgres://svc:pw@localhost:5432/db", DBName: "my db/x"},
			wantUser:     "svc",
			wantPassword: "pw",
			wantDatabase: "my db/x",
			wantHost:     "localhost",
			wantPort:     5432,
		},
		"port override is the integer": {
			given:        dbstore.Config{URL: "postgres://svc:pw@localhost:5432/db", Port: launderInt(7777)},
			wantUser:     "svc",
			wantPassword: "pw",
			wantDatabase: "db",
			wantHost:     "localhost",
			wantPort:     7777,
		},
		"port override on IPv6 host": {
			given:        dbstore.Config{URL: "postgres://svc:pw@[::1]:5432/db", Port: launderInt(7777)},
			wantUser:     "svc",
			wantPassword: "pw",
			wantDatabase: "db",
			wantHost:     "::1",
			wantPort:     7777,
		},
		"cloud sql socket host is preserved": {
			given: dbstore.Config{
				URL:      "postgres://svc@localhost/db?host=/cloudsql/project:region:instance&sslmode=disable",
				Password: awkward,
			},
			wantUser:     "svc",
			wantPassword: awkward,
			wantDatabase: "db",
			wantHost:     "/cloudsql/project:region:instance",
			wantPort:     5432,
		},
		"query options are kept": {
			given: dbstore.Config{
				URL:      "postgres://svc@localhost:5432/db?application_name=my%20app&search_path=a,b",
				Password: "pw",
			},
			wantUser:     "svc",
			wantPassword: "pw",
			wantDatabase: "db",
			wantHost:     "localhost",
			wantPort:     5432,
			wantRuntime:  map[string]string{"application_name": "my app", "search_path": "a,b"},
		},
		"overrides win over query options": {
			given: dbstore.Config{
				URL:      "postgres://localhost:5432/db?user=q&password=q&dbname=q&port=1",
				User:     "svc",
				Password: "pw",
				DBName:   "app",
				Port:     launderInt(7777),
			},
			wantUser:     "svc",
			wantPassword: "pw",
			wantDatabase: "app",
			wantHost:     "localhost",
			wantPort:     7777,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			d, err := openDialector(tc.given)
			require.NoError(t, err)

			cfg, err := pgconn.ParseConfig(postgresDSN(t, d))
			require.NoError(t, err)
			require.Equal(t, tc.wantUser, cfg.User)
			require.Equal(t, tc.wantPassword, cfg.Password)
			require.Equal(t, tc.wantDatabase, cfg.Database)
			require.Equal(t, tc.wantHost, cfg.Host)
			require.Equal(t, tc.wantPort, cfg.Port)
			for k, v := range tc.wantRuntime {
				require.Equal(t, v, cfg.RuntimeParams[k], k)
			}
		})
	}
}

// TestConfig_DialectorLiveWrongPassword checks that a connection-time
// failure from a real server does not report the password either.
func TestConfig_DialectorLiveWrongPassword(t *testing.T) {
	serverURL := os.Getenv(pgtest.EnvURL)
	if serverURL == "" {
		t.Skipf("%s is not set; skipping a test that needs Postgres", pgtest.EnvURL)
	}

	u, err := url.Parse(serverURL)
	require.NoError(t, err)
	user := "postgres"
	if u.User != nil && u.User.Username() != "" {
		user = u.User.Username()
	}
	u.User = nil

	d, err := dbstore.Config{URL: u.String(), User: user, Password: leakSecret}.Dialector()
	require.NoError(t, err)

	_, err = gorm.Open(d, &gorm.Config{Logger: logger.Discard})
	require.Error(t, err, "the server accepted a wrong password")
	requireNoSecret(t, err, leakSecret)
}
