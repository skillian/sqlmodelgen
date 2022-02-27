package sqlmodelgen

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unsafe"

	"github.com/skillian/expr/errors"
	"github.com/skillian/expr/stream/sqlstream/sqltypes"
)

var (
	// PUWVJSONModelContext produces a Paperless.Unity WorkView
	// JSON model from a source model.
	PUWVJSONModelContext interface {
		ModelContext
		TemplateDataWriter
	} = puWVJSONContext{}
)

type puWVJSONContext struct{}

var _ interface {
	ModelContext
	TemplateDataWriter
} = puWVJSONContext{}

func (puWVJSONContext) ModelType(t sqltypes.Type) (namespace, typename string, err error) {
	typename, _, err = wvAceType(t)
	return
}

type puWVJSONRoot struct {
	Namespace    string
	Applications []puWVJSONApplication
}

type puWVJSONApplication struct {
	Name      string
	Namespace string
	Classes   []puWVJSONClass
}

type puWVJSONClass struct {
	Name       string
	ID         int64
	Attributes []puWVJSONAttribute
}

type puWVJSONAttribute struct {
	Name           string
	ID             int64
	ClassID        int64
	Type           string
	Length         int64 `json:",omitempty"`
	RelatedClassID int64 `json:",omitempty"`
}

func (puWVJSONContext) WriteTemplateData(w io.Writer, td TemplateData) (err error) {
	r := puWVJSONRoot{Namespace: td.Namespace}
	for _, db := range td.MetaModel.Databases {
		r.Applications = append(r.Applications, puWVJSONApplication{
			Name:      strings.ToTitle(db.RawName),
			Namespace: db.ModelName,
		})
		wvApp := &r.Applications[len(r.Applications)-1]
		for _, sch := range db.Schemas {
			for _, tbl := range sch.Tables {
				wvApp.Classes = append(wvApp.Classes, puWVJSONClass{
					Name: tbl.ModelName,
					ID:   int64(uintptr(unsafe.Pointer(tbl))),
				})
				wvCls := &wvApp.Classes[len(wvApp.Classes)-1]
				for _, col := range tbl.Columns {
					wvCls.Attributes = append(wvCls.Attributes, puWVJSONAttribute{
						Name:    col.ModelName,
						ID:      int64(uintptr(unsafe.Pointer(col))),
						ClassID: wvCls.ID,
					})
					wvAttr := &wvCls.Attributes[len(wvCls.Attributes)-1]
					if col.FK != nil {
						wvAttr.Type = "Relationship"
						wvAttr.RelatedClassID = int64(uintptr(unsafe.Pointer(col.FK.Column.Table)))
						continue
					}
					t := col.Type
					if sqltypes.IsNullable(t) {
						t = t.(sqltypes.Nullable)[0]
					}
					switch t := t.(type) {
					case sqltypes.BoolType:
						wvAttr.Type = "Boolean"
					case sqltypes.DecimalType:
						wvAttr.Type = "Decimal"
					case sqltypes.FloatType:
						wvAttr.Type = "Floating Point"
					case sqltypes.IntType:
						wvAttr.Type = "Integer"
					case sqltypes.StringType:
						if t.Var {
							wvAttr.Type = "Text"
						} else {
							wvAttr.Type = "Alphanumeric"
							wvAttr.Length = int64(t.Length)
						}
					case sqltypes.TimeType:
						if t.Prec >= 24*time.Hour {
							wvAttr.Type = "Date"
						} else {
							wvAttr.Type = "Date/Time"
						}
					}
				}
			}
		}
	}
	bs, err := json.MarshalIndent(r, "", "\t")
	if err != nil {
		return errors.Errorf0From(
			err,
			"failed to marshal model into "+
				"Paperless.Unity WorkView JSON",
		)
	}
	if _, err = io.Copy(w, bytes.NewReader(bs)); err != nil {
		return errors.Errorf2From(
			err, "failed to copy %[1]d bytes to "+
				"%[2]v (type: %[2]T)",
			len(bs), w,
		)
	}
	return nil
}
