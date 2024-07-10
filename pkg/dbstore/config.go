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
	ErrUnsupportedDialect = errors.New("unsupported data source type")
)

type StoreConfig struct {
	DSN      string `help:"Data Source Name"`
	URL      string `help:"Connection string" default:"sqlite:test.sqlite"`
	User     string `help:"Username used to authenticate to a Data Source" env:"DB_USER"`
	Password string `help:"Password used to authenticate to a Data Source" env:"DB_PASS"`

	DBName string `help:"Name of the DB to connect to if Data Source supports multiple DBs" env:"DB_NAME"`
	Port   int    `help:"A port to connect to Data Source"`
}

func (c StoreConfig) Dialector() (gorm.Dialector, error) {
	if c.DSN == "" && c.URL == "" {
		return nil, ErrNoDSNorURL
	}

	u, err := dburl.Parse(c.URL)
	if err != nil {
		return nil, err
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
	if c.Port != 0 {
		extraParams = append(extraParams, fmt.Sprintf("port=%v", c.Port))
	}

	dsn := strings.Join(extraParams, " ")
	switch u.Driver {
	case "sqlite3":
		return sqlite.Open(u.DSN), nil
	case "mysql":
		return mysql.Open(u.DSN), nil
	case "postgres":
		return postgres.Open(dsn), nil
	default:
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedDialect, u.Driver)
	}
}
