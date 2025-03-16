package dbstore

import (
	"errors"
	"fmt"
	"strings"

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
func (c Config) Dialector() (gorm.Dialector, error) {
	if c.DSN == "" && c.URL == "" {
		return nil, ErrNoDSNorURL
	}

	u, err := dburl.Parse(c.URL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDBUrl, err)
	}

	// convert custom params to DSN:
	extraParams := []string{u.DSN}
	if c.DSN != "" {
		extraParams = append(extraParams, c.DSN)
	}
	if c.User != "" {
		extraParams = append(extraParams, fmt.Sprintf("user=%v", c.User))
	}
	if c.Password != "" {
		extraParams = append(extraParams, fmt.Sprintf("password=%v", c.Password))
	}
	if c.DBName != "" {
		extraParams = append(extraParams, fmt.Sprintf("dbname=%v", c.DBName))
	}
	if c.Port != nil {
		extraParams = append(extraParams, fmt.Sprintf("port=%v", c.Port))
	}

	dsn := strings.Join(extraParams, " ")
	switch u.Driver {
	case "sqlite3", "sqlite":
		return sqlite.Open(u.DSN), nil
	case "mysql":
		return mysql.Open(u.DSN), nil
	case "postgres":
		return postgres.Open(dsn), nil
	default:
		// return sql.Open(dsn)
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedDialect, u.Driver)
	}
}
