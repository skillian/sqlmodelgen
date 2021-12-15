package sqlmodelgen

import (
	"io"
	"strconv"
	"time"

	"github.com/skillian/expr/errors"
	"github.com/skillian/expr/stream/sqlstream"
	"github.com/skillian/expr/stream/sqlstream/sqltypes"
	"github.com/xuri/excelize/v2"
)

var (
	WVAceModelContext interface {
		ModelContext
		MetaModelWriter
	} = wvAceModelContext{}
)

type wvAceModelContext struct{}

var _ interface {
	ModelContext
	MetaModelWriter
} = wvAceModelContext{}

func (wvAceModelContext) ModelType(t sqltypes.Type) (namespace, typename string, err error) {
	typename, _, err = wvAceType(t)
	return
}

func (wvAceModelContext) WriteMetaModel(w io.Writer, c *sqlstream.MetaModel) (err error) {
	if len(c.Databases) == 0 {
		return errors.Errorf("at least one database is required")
	}
	if len(c.Databases) > 1 {
		return errors.Errorf("ACE files can only define one database")
	}
	db := c.Databases[0]
	f := excelize.NewFile()
	s := wvAceClassSheet{}
	for _, sch := range db.Schemas {
		for _, tbl := range sch.Tables {
			wvClassName := wvAceClassName(tbl)
			createWVAceClassSheet(f, wvClassName)
			s.Filters = "All " + wvClassName
			s.Views = wvClassName
			s.Sections = wvClassName
			for i, col := range tbl.Columns {
				s.DisplayName = col.ModelName
				s.DataType, s.LengthPrecision, err = wvAceType(col.Type)
				if err != nil {
					return errors.Errorf1From(
						err, "error getting WorkView "+
							"ACE data type of "+
							"column %v",
						col,
					)
				}
				s.RelatedClass = ""
				if col.FK != nil {
					fkCol := col.FK.Column
					if !fkCol.PK {
						return errors.Errorf(
							"column %[1]v of table %[2]v "+
								"references column %[3]v of %[4]v "+
								"but %[3]v of %[4]v "+
								"is not a primary key",
							col, tbl, fkCol, fkCol.Table,
						)
					}
					s.RelatedClass = wvAceClassName(fkCol.Table)
					s.DataType = "Relation"
				}
				// TODO: Column documentation
				// TODO: Column datasets?
				// TODO: Column default values?
				s.PrimaryAttribute = col.PK
				if err = s.writeRow(f, wvClassName, i+2); err != nil {
					return errors.Errorf2From(
						err, "error while writing "+
							"row for column %v "+
							"of table %v",
						col, tbl,
					)
				}
			}
		}
	}
	// delete default sheet
	f.DeleteSheet(f.GetSheetName(0))
	if _, err = f.WriteTo(w); err != nil {
		return errors.Errorf1From(
			err, "failed to write Excel file to %v", w,
		)
	}
	return nil
}

var wvAceClassSheetHeaders = []string{
	"Display Name",
	"Data Type",
	"Related Class",
	"Length / Precision",
	"Description",
	"Data Set",
	"Default Value",
	"Index",
	"Filters",
	"Views",
	"Sections",
	"Primary Attribute",
}

func createWVAceClassSheet(f *excelize.File, name string) {
	_ = f.NewSheet(name)
	for i, h := range wvAceClassSheetHeaders {
		f.SetCellStr(name, excelColumn(i)+"1", h)
	}
}

func excelColumn(index int) string {
	// Add AA..ZZ, etc. support if ever needed
	if index >= 26 {
		panic("only 26 columns supported")
	}
	return string('A' + rune(index))
}

func wvAceType(t sqltypes.Type) (dataType string, lengthPrecision int, err error) {
	switch t := t.(type) {
	case sqltypes.Nullable:
		return wvAceType(t[0])
	case sqltypes.BoolType:
		return "Boolean", 0, nil
	case sqltypes.IntType:
		return "Integer", 0, nil
	case sqltypes.FloatType:
		return "Floating Point", 0, nil
	case sqltypes.DecimalType:
		return "Decimal", t.Prec, nil
	case sqltypes.StringType:
		if t.Var {
			return "Text", 0, nil
		}
		if t.Length < 256 {
			return "Alphanumeric", t.Length, nil
		}
		return "Text", 0, nil
	case sqltypes.TimeType:
		if t.Prec >= 24*time.Hour {
			return "Date", 0, nil
		}
		return "Date/Time", 0, nil
	case sqltypes.BytesType:
		if t.Var {
			return "Text", 0, nil
		}
		if t.Length < 256 {
			return "Alphanumeric", 0, nil
		}
		return "Text", 0, nil
	}
	return "", 0, errors.Errorf1(
		"Unknown model type: %[1]v (type: %[1]T)",
		t,
	)
}

func wvAceClassName(tbl *sqlstream.Table) string {
	return tbl.Schema.ModelName + tbl.ModelName
}

type wvAceClassSheet struct {
	DisplayName      string
	DataType         string
	RelatedClass     string
	LengthPrecision  int
	Description      string
	DataSet          string
	DefaultValue     string
	Index            string
	Filters          string
	Views            string
	Sections         string
	PrimaryAttribute bool
}

func (s *wvAceClassSheet) writeRow(f *excelize.File, sheet string, index int) (err error) {
	ixstr := strconv.Itoa(index)
	if err = f.SetCellStr(sheet, "A"+ixstr, s.DisplayName); err != nil {
		return err
	}
	if err = f.SetCellStr(sheet, "B"+ixstr, s.DataType); err != nil {
		return err
	}
	if err = f.SetCellStr(sheet, "C"+ixstr, s.RelatedClass); err != nil {
		return err
	}
	if s.LengthPrecision == 0 {
		if err = f.SetCellStr(sheet, "D"+ixstr, ""); err != nil {
			return err
		}
	} else {
		if err = f.SetCellInt(sheet, "D"+ixstr, s.LengthPrecision); err != nil {
			return err
		}
	}
	if err = f.SetCellStr(sheet, "E"+ixstr, s.Description); err != nil {
		return err
	}
	if err = f.SetCellStr(sheet, "F"+ixstr, s.DataSet); err != nil {
		return err
	}
	if err = f.SetCellStr(sheet, "G"+ixstr, s.DefaultValue); err != nil {
		return err
	}
	if err = f.SetCellStr(sheet, "H"+ixstr, s.Index); err != nil {
		return err
	}
	if err = f.SetCellStr(sheet, "I"+ixstr, s.Filters); err != nil {
		return err
	}
	if err = f.SetCellStr(sheet, "J"+ixstr, s.Views); err != nil {
		return err
	}
	if err = f.SetCellStr(sheet, "K"+ixstr, s.Sections); err != nil {
		return err
	}
	if err = f.SetCellBool(sheet, "L"+ixstr, s.PrimaryAttribute); err != nil {
		return err
	}
	return nil
}
