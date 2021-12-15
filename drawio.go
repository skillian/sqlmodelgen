package sqlmodelgen

import (
	"io"

	"github.com/skillian/expr/errors"
	"github.com/skillian/expr/stream/sqlstream"
	"github.com/skillian/expr/stream/sqlstream/sqltypes"
	"github.com/xuri/excelize/v2"
)

var (
	DrawIOModelContext interface {
		ModelContext
		MetaModelWriter
	} = drawIOModelContext{}
)

type drawIOModelContext struct{}

func (drawIOModelContext) ModelType(t sqltypes.Type) (namespace, typename string, err error) {
	return "", t.String(), nil
}

func (drawIOModelContext) WriteMetaModel(w io.Writer, c *sqlstream.MetaModel) (err error) {
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
