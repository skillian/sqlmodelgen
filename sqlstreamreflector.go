package sqlmodelgen

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"io/ioutil"

	"github.com/skillian/expr"
	"github.com/skillian/expr/errors"
	"github.com/skillian/expr/stream/sqlstream"
	"github.com/skillian/expr/stream/sqlstream/config"
	"github.com/skillian/expr/stream/sqlstream/sqltypes"
)

var (
	SQLStreamReflectorModelContext interface {
		ModelContext
		ModelConfigParser
	} = sqlStreamReflectorModelContext{}
)

type sqlStreamReflectorModelContext struct{}

func (sqlStreamReflectorModelContext) ModelType(t sqltypes.Type) (namespace, typename string, err error) {
	return t.String(), "", nil
}

func (sqlStreamReflectorModelContext) ParseModelConfig(ctx context.Context, r io.Reader) (cfg config.Config, err error) {
	type ConnectionConfig struct {
		DriverName       string
		ConnectionString string
		DialectName      string
		ReflectorName    string
	}
	c := ConnectionConfig{}
	{
		bs, err := ioutil.ReadAll(r)
		if err != nil {
			return cfg, errors.Errorf1From(
				err, "failed to read all data from %v", r,
			)
		}
		if err = json.Unmarshal(bs, &c); err != nil {
			return cfg, errors.Errorf0From(
				err, "failed to unmarshal connection "+
					"configuration JSON",
			)
		}
	}
	d, err := sqlstream.ParseDialect(c.DialectName)
	if err != nil {
		return cfg, errors.Errorf1From(
			err, "error parsing dialect: %q", c.DialectName,
		)
	}
	re, err := sqlstream.ParseReflector(c.ReflectorName)
	if err != nil {
		return cfg, errors.Errorf1From(
			err, "error parsing reflector: %q", c.ReflectorName,
		)
	}
	sqlDB, err := sql.Open(c.DriverName, c.ConnectionString)
	if err != nil {
		return cfg, errors.Errorf0From(
			err, "failed to establish SQL connection",
		)
	}
	db, err := sqlstream.NewDB(sqlDB, sqlstream.WithDialect(d))
	if err != nil {
		return cfg, errors.Errorf2From(
			err, "error creating sqlstream database from %v, "+
				"with dialect %v",
			sqlDB, d,
		)
	}
	ctx, _ = expr.ValuesFromContextOrNew(context.Background())
	cfg, err = re.Config(ctx, db)
	if err != nil {
		return cfg, errors.Errorf1From(
			err, "failed to create configuration from database %v",
			db,
		)
	}
	return
}
