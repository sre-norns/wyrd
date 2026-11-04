package dbstore

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/xo/dburl"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var (
	ErrNoDSNorURL         = errors.New("no Data Source connection details provided")
	ErrInvalidDBUrl       = errors.New("invalid DB URL")
	ErrUnsupportedDialect = errors.New("unsupported data source type")
)

// Config struct defines common command line options to select and connect to the DB implementing store.
type Config struct {
	DSN string `help:"Data Source Name" group:"DB_STORE_DNS"`

	URL      string `help:"Connection string" default:"sqlite:test.sqlite" group:"DB_STORE_URL" env:"DB_URL"`
	User     string `help:"Username used to authenticate to a Data Source" group:"DB_STORE_URL" env:"DB_USER"`
	Password string `help:"Password used to authenticate to a Data Source" group:"DB_STORE_URL" env:"DB_PASS"`

	DBName string `help:"Name of the DB to connect to if Data Source supports multiple DBs" group:"DB_STORE_URL" env:"DB_NAME"`
	Port   *int   `help:"A port to connect to a Data Source" group:"DB_STORE_URL"`
}

// Dialector produces [gorm.Dialector] based on the config options selected.
// This allows for flexible implementation where DB driver is selected based on the DB Url and connection parameters.
//
// The URL and the overrides carry credentials, so no error returned here
// quotes or wraps them: parser errors are replaced by fixed messages. For
// Postgres the final DSN is validated here, so that gorm.Open does not fail
// later on its syntax with an error of its own.
func (c Config) Dialector() (gorm.Dialector, error) {
	if c.DSN == "" && c.URL == "" {
		return nil, ErrNoDSNorURL
	}

	u, err := dburl.Parse(c.URL)
	if err != nil {
		return nil, sanitizeURLError(err)
	}

	// Matched on the dialect, not on the Go driver xo/dburl would use: from
	// dburl v0.32 a postgres:// URL reports the "pgx" driver, and matching
	// Driver alone left every Postgres URL unsupported.
	switch u.Dialect {
	case "sqlite3", "sqlite":
		return sqlite.Open(u.DSN), nil
	case "mysql":
		return mysql.Open(u.DSN), nil
	case "postgres", "pgx":
		dsn, err := c.postgresDSN(u.DSN)
		if err != nil {
			return nil, err
		}
		return postgres.Open(dsn), nil
	default:
		// u.Driver is a driver name from the dburl scheme table, never input.
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedDialect, u.Driver)
	}
}

// sanitizeURLError maps a dburl.Parse failure to an error that holds no part
// of the input. The dburl.Error constants are fixed strings and safe to keep.
// A *url.Error, and its cause, quote the input, and so are never formatted or
// wrapped.
func sanitizeURLError(err error) error {
	if dbErr, ok := errors.AsType[dburl.Error](err); ok {
		return fmt.Errorf("%w: %s", ErrInvalidDBUrl, string(dbErr))
	}

	if _, ok := errors.AsType[url.EscapeError](err); ok {
		return fmt.Errorf("%w: invalid percent-encoding; write a literal %% in the user name or password as %%25", ErrInvalidDBUrl)
	}

	return fmt.Errorf("%w: cannot parse the URL; percent-encode reserved characters (such as / # ? @ : %%) in the user name and password", ErrInvalidDBUrl)
}

// errPostgresSettings is returned when the final Postgres DSN does not parse.
// The pgx error is dropped: its message and its fields may hold the password.
var errPostgresSettings = fmt.Errorf("%w: the Postgres connection settings do not parse; check the URL query options and the port", ErrInvalidDBUrl)

// postgresDSN applies the User, Password, DBName and Port overrides to base,
// the URL-form DSN that dburl generated, and validates the result.
//
// The overrides are set on the parsed URL, so that net/url encodes every value.
// Each override also drops the query option that pgx would read in its place,
// so the override wins. base is returned unchanged when there is no override.
func (c Config) postgresDSN(base string) (string, error) {
	if c.DSN != "" {
		// A keyword/value DSN cannot be appended to a URL; pgx rejects the mix.
		return "", fmt.Errorf("%w: a Postgres URL cannot be combined with a DSN; put the options in the URL query", ErrInvalidDBUrl)
	}

	dsn := base
	if c.User != "" || c.Password != "" || c.DBName != "" || c.Port != nil {
		pu, err := url.Parse(base)
		if err != nil {
			return "", errPostgresSettings
		}

		var drop []string
		if c.User != "" || c.Password != "" {
			user, pass, hasPass := c.User, c.Password, c.Password != ""
			if pu.User != nil {
				if user == "" {
					user = pu.User.Username()
				}
				if !hasPass {
					pass, hasPass = pu.User.Password()
				}
			}
			if hasPass {
				pu.User = url.UserPassword(user, pass)
			} else {
				pu.User = url.User(user)
			}
		}
		if c.User != "" {
			drop = append(drop, "user")
		}
		if c.Password != "" {
			drop = append(drop, "password")
		}
		if c.DBName != "" {
			pu.Path, pu.RawPath = "/"+c.DBName, ""
			drop = append(drop, "dbname", "database")
		}

		var port string
		if c.Port != nil {
			port = strconv.Itoa(*c.Port)
			drop = append(drop, "port")
		}

		pu.RawQuery = dropQueryKeys(pu.RawQuery, drop)
		if port != "" {
			if host := pu.Hostname(); host != "" && !strings.Contains(pu.Host, ",") {
				pu.Host = net.JoinHostPort(host, port)
			} else {
				// No URL host (a unix socket in the query) or several hosts:
				// the query option gives one port for every host.
				pu.RawQuery = joinQuery(pu.RawQuery, "port="+port)
			}
		}
		dsn = pu.String()
	}

	// The same parse gorm.Open runs. It reads files and the environment, and
	// opens no connection.
	if _, err := pgx.ParseConfig(dsn); err != nil {
		return "", errPostgresSettings
	}
	return dsn, nil
}

// dropQueryKeys removes each option named in keys from rawQuery, and keeps
// every other option as it was encoded.
func dropQueryKeys(rawQuery string, keys []string) string {
	if rawQuery == "" || len(keys) == 0 {
		return rawQuery
	}

	var kept []string
	for pair := range strings.SplitSeq(rawQuery, "&") {
		key, _, _ := strings.Cut(pair, "=")
		if k, err := url.QueryUnescape(key); err == nil {
			key = k
		}
		if !slices.Contains(keys, key) {
			kept = append(kept, pair)
		}
	}
	return strings.Join(kept, "&")
}

func joinQuery(rawQuery, pair string) string {
	if rawQuery == "" {
		return pair
	}
	return rawQuery + "&" + pair
}
