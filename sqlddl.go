package sqlmodelgen

import (
	"embed"
	"io/fs"

	"github.com/skillian/expr/errors"
	"github.com/skillian/expr/stream/sqlstream"
	"github.com/skillian/expr/stream/sqlstream/sqltypes"
)

var (
	MSSQLDDLModelContext     = mustNewSQLDDLModelContext("mssql")
	SQLiteSQLDDLModelContext = mustNewSQLDDLModelContext("sqlite3")

	_ interface {
		ModelContext
		TemplateContext
	} = sqlDDLModelContext{}

	//go:embed sqlddl
	sqlDDLFS embed.FS

	sqlDDLModelFs fs.FS = func() fs.FS {
		fsys, err := fs.Sub(sqlDDLFS, "sqlddl")
		if err != nil {
			panic(err)
		}
		return fsys
	}()
)

// sqlDDLModelContext is the implementation of the Go language model generator.
type sqlDDLModelContext struct {
	dialectName string
	dialect     sqlstream.Dialect
}

func mustNewSQLDDLModelContext(dialectName string) *sqlDDLModelContext {
	d, err := sqlstream.ParseDialect(dialectName)
	if err != nil {
		panic(errors.Errorf1From(
			err, "failed to parse dialect %q",
			dialectName,
		))
	}
	return &sqlDDLModelContext{
		dialectName: dialectName,
		dialect:     d,
	}
}

func (mc sqlDDLModelContext) FS() fs.FS {
	fsys, err := fs.Sub(sqlDDLModelFs, mc.dialectName)
	if err != nil {
		panic(errors.Errorf1From(
			err, "failed to get subdirectory %q",
			mc.dialectName,
		))
	}
	return fsys
}

// ModelType produces Go data types from sqltype.Type definitions.
func (mc sqlDDLModelContext) ModelType(t sqltypes.Type) (namespace, typename string, err error) {
	typename, err = mc.dialect.DataTypeName(t)
	return
}
