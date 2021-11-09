package sqlmodelgen

import (
	"embed"
	"io/fs"
	"text/template"

	"github.com/skillian/expr/errors"
	"github.com/skillian/expr/stream/sqlstream/sqltypes"
)

var (
	// CSModelContext is the C# language model context.
	CSModelContext interface {
		ModelContext
		TemplateContext
	} = csModelContext{}

	//go:embed cs/*.txt
	csFs embed.FS

	csModelFs fs.FS = func() fs.FS {
		fsys, err := fs.Sub(csFs, "cs")
		if err != nil {
			panic(err)
		}
		return fsys
	}()
)

type csModelContext struct{}

func (csModelContext) FS() fs.FS { return csModelFs }

func (csModelContext) ModelType(t sqltypes.Type) (namespace, typename string, err error) {
	switch t := t.(type) {
	case sqltypes.Nullable:
		namespace, typename, err = CSModelContext.ModelType(t[0])
		if err != nil {
			return
		}
		if typename == "string" {
			return
		}
		typename += "?"
		return
	case sqltypes.BoolType:
		return "", "bool", nil
	case sqltypes.IntType:
		switch {
		case t.Bits <= 8:
			return "", "byte", nil
		case t.Bits <= 16:
			return "", "short", nil
		case t.Bits <= 32:
			return "", "int", nil
		case t.Bits <= 64:
			return "", "long", nil
		}
		return "", "", errors.Errorf1(
			"int with %d bits not supported",
			t.Bits)
	case sqltypes.FloatType:
		switch {
		case t.Mantissa <= 24:
			return "", "float", nil
		case t.Mantissa <= 53:
			return "", "double", nil
		}
		return "", "", errors.Errorf1(
			"float with %d mantissa bits not "+
				"supported", t.Mantissa)
	case sqltypes.StringType:
		return "", "string", nil
	case sqltypes.TimeType:
		return "", "DateTime", nil
	case sqltypes.BytesType:
		return "", "byte[]", nil
	}
	return "", "object", nil
}

func (csModelContext) AddFuncs(m template.FuncMap) {
	m["datareaderfunc"] = csDataReaderFunc
}

func csDataReaderFunc(t sqltypes.Type) (string, error) {
	switch t := t.(type) {
	case sqltypes.Nullable:
		return csDataReaderFunc(t[0])
	case sqltypes.BoolType:
		return "GetBoolean", nil
	case sqltypes.IntType:
		switch {
		case t.Bits <= 8:
			return "GetByte", nil
		case t.Bits <= 16:
			return "GetInt16", nil
		case t.Bits <= 32:
			return "GetInt32", nil
		case t.Bits <= 64:
			return "GetInt64", nil
		}
		return "", errors.Errorf1(
			"no known function call to get %d-bit integer",
			t.Bits,
		)
	case sqltypes.TimeType:
		return "GetDateTime", nil
	case sqltypes.DecimalType:
		return "GetDecimal", nil
	case sqltypes.FloatType:
		if t.Mantissa <= 24 {
			return "GetFloat", nil
		}
		return "GetDouble", nil
	case sqltypes.StringType:
		return "GetString", nil
	}
	return "", errors.Errorf1(
		"unknown call to retrieve %v from IDataReader",
		t,
	)
}
