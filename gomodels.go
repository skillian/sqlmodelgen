package sqlmodelgen

import (
	"embed"
	"io/fs"
	"sort"
	"strings"

	"github.com/skillian/expr/errors"
	"github.com/skillian/expr/stream/sqlstream/sqltypes"
)

var (
	// GoModelsModelContext defines the ModelContext that generates models
	// for the Go programming language.
	GoModelsModelContext interface {
		ModelContext
		TemplateContext
	} = goModelsModelContext{}

	//go:embed gomodels/*.txt
	goModelsFs embed.FS

	goModelsModelFs fs.FS = func() fs.FS {
		fsys, err := fs.Sub(goModelsFs, "gomodels")
		if err != nil {
			panic(err)
		}
		return fsys
	}()
)

// goModelsModelContext is the implementation of the Go language model generator.
type goModelsModelContext struct{}

func (goModelsModelContext) FS() fs.FS { return goModelsModelFs }

// ModelType produces Go data types from sqltype.Type definitions.
func (goModelsModelContext) ModelType(t sqltypes.Type) (namespace, typename string, err error) {
	switch t := t.(type) {
	case sqltypes.Nullable:
		namespace, typename, err = GoSQLModelContext.ModelType(t[0])
		if err != nil {
			return
		}
		switch typename {
		case "bool":
			namespace, typename = "database/sql", "sql.NullBool"
		case "float64":
			namespace, typename = "database/sql", "sql.NullFloat64"
		case "int32", "int16", "int8":
			namespace, typename = "database/sql", "sql.NullInt32"
		case "int64":
			namespace, typename = "database/sql", "sql.NullInt64"
		case "string":
			namespace, typename = "database/sql", "sql.NullString"
		case "time.Time":
			namespace, typename = "database/sql", "sql.NullTime"
		}
		return
	case sqltypes.BoolType:
		return "", "bool", nil
	case sqltypes.IntType:
		switch {
		case t.Bits <= 8:
			return "", "int8", nil
		case t.Bits <= 16:
			return "", "int16", nil
		case t.Bits <= 32:
			return "", "int32", nil
		case t.Bits <= 64:
			return "", "int64", nil
		}
		return "", "", errors.Errorf1(
			"int with %d bits not supported",
			t.Bits)
	case sqltypes.FloatType:
		switch {
		case t.Mantissa <= 24:
			return "", "float32", nil
		case t.Mantissa <= 53:
			return "", "float64", nil
		}
		return "", "", errors.Errorf1(
			"float with %d mantissa bits not "+
				"supported", t.Mantissa)
	case sqltypes.StringType:
		return "", "string", nil
	case sqltypes.TimeType:
		return "time", "time.Time", nil
	case sqltypes.BytesType:
		return "", "[]byte", nil
	}
	return "", "interface{}", nil
}

func (goModelsModelContext) OrganizeNamespaces(nss []string) []string {
	stdlib := make([]string, 0, len(nss))
	external := make([]string, 0, len(nss))
	for _, ns := range nss {
		pivot := strings.IndexByte(ns, '/')
		if pivot == -1 {
			stdlib = append(stdlib, ns)
			continue
		}
		firstPart := ns[:pivot]
		if strings.ContainsRune(firstPart, '.') {
			// assume it's a host name and therefore external
			external = append(external, ns)
			continue
		}
		// otherwise, it's a deep path to a stdlib
		stdlib = append(stdlib, ns)
	}
	sort.Strings(stdlib)
	sort.Strings(external)
	nss = nss[:0]
	if len(stdlib) > 0 {
		nss = append(nss, stdlib...)
		if len(external) > 0 {
			nss = append(nss, "") // gap
		}
	}
	return append(nss, external...)
}
