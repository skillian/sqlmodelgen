package sqlmodelgen

import (
	"io"
	"strings"
	"text/template"

	"github.com/skillian/expr/errors"
	"github.com/skillian/expr/stream/sqlstream"
	"github.com/skillian/expr/stream/sqlstream/sqltypes"
	"github.com/skillian/textwrap"
)

// Pair bundles together two arbitrary values.  It is intended to be
// used from the Dict type.
type Pair [2]interface{}

// pair is added to the template's funcmap with the AddFuncs function.
func pair(a, b interface{}) Pair { return Pair{a, b} }

// Dict maps an arbitrary key to a value in templates.
type Dict map[interface{}]interface{}

// dict is added to a template's funcmap with the AddFuncs function
// so that templates can define parameter mappings.
func dict(pairs ...Pair) (d Dict) {
	d = make(Dict, len(pairs))
	for _, p := range pairs {
		d[p[0]] = p[1]
	}
	return
}

type Set map[interface{}]struct{}

func set(elements ...interface{}) (s Set) {
	s = make(Set, len(elements))
	for _, e := range elements {
		s[e] = struct{}{}
	}
	return
}

func (s Set) Contains(e interface{}) (ok bool) {
	_, ok = s[e]
	return
}

// CreateDynTemplate creates a "dyntemplate" function whose
// template name is parameterized
func CreateDynTemplate(t *template.Template) (dyntemplate func(name string, data interface{}) (string, error)) {
	return func(name string, data interface{}) (string, error) {
		var b strings.Builder
		if err := t.ExecuteTemplate(&b, name, data); err != nil {
			return "", errors.Errorf1From(
				err, "error while executing dynamic "+
					"template: %q", name)
		}
		return b.String(), nil
	}
}

// AddFuncs adds sqlmodelgen's template functions to a FuncMap.
// It will not overwrite existing keys.
func AddFuncs(t *template.Template, m template.FuncMap, mc ModelContext) *template.Template {
	add := func(m template.FuncMap, k string, v interface{}) {
		if _, ok := m[k]; !ok {
			m[k] = v
		}
	}
	if _, ok := m["dyntemplate"]; !ok {
		add(m, "dyntemplate", CreateDynTemplate(t))
	}
	type modelColumn struct {
		Kind   string
		Path   string
		Column *sqlstream.Column
	}
	add(m, "allmodelcolumns", func(t *sqlstream.Table) (res []modelColumn) {
		res = make([]modelColumn, 0, len(t.Columns))
	columnLoop:
		for _, c := range t.Columns {
			if t.PK != nil && t.PK.Column == c {
				res = append(res, modelColumn{
					Kind:   "pk",
					Path:   t.PK.ModelName,
					Column: c,
				})
				continue
			}
			if t.Key != nil {
				for _, id := range t.Key.IDs {
					if id.Column == c {
						res = append(res, modelColumn{
							Kind: "key",
							Path: strings.Join([]string{
								t.Key.ModelName,
								id.ModelName,
							}, "."),
							Column: c,
						})
						continue columnLoop
					}
				}
			}
			if c.FK != nil {
				res = append(res, modelColumn{
					Kind:   "fk",
					Path:   c.ModelName,
					Column: c,
				})
				continue
			}
			res = append(res, modelColumn{
				Kind:   "",
				Path:   c.ModelName,
				Column: c,
			})
		}
		return
	})
	add(m, "pair", pair)
	add(m, "dict", dict)
	add(m, "set", set)
	if _, ok := m["modeltype"]; !ok {
		add(m, "modeltype", func(t sqltypes.Type) (name string, err error) {
			_, name, err = mc.ModelType(t)
			return
		})
	}
	add(m, "isnullable", sqltypes.IsNullable)
	add(m, "basemodeltype", func(t sqltypes.Type) (name string, err error) {
		_ = sqltypes.IterInners(t, func(x sqltypes.Type) error {
			t = x
			// Doesn't matter what gets returned here.
			// Just break after the first iteration.
			return io.EOF
		})
		_, name, err = mc.ModelType(t)
		return
	})
	specialPluralEndings := []string{"s", "x", "z"}
	add(m, "pluralize", func(name string) string {
		if strings.HasSuffix(name, "y") {
			return name[:len(name)-1] + "ies"
		}
		for _, suffix := range specialPluralEndings {
			if strings.HasSuffix(name, suffix) {
				return name + "es"
			}
		}
		return name + "s"
	})
	add(m, "isassoctable", func(t *sqlstream.Table) bool {
		if len(t.Columns) != 2 {
			return false
		}
		return t.Columns[0].FK != nil && t.Columns[1].FK != nil
	})
	add(m, "assockey", func(c *sqlstream.Column) (*sqlstream.Column, error) {
		if !m["isassoctable"].(func(*sqlstream.Table) bool)(c.Table) {
			return nil, errors.Errorf2(
				"column %v's table, %v, is not an "+
					"association table",
				c, c.Table,
			)
		}
		if c.Table.Columns[0] == c {
			return c.Table.Columns[1], nil
		}
		return c.Table.Columns[0], nil
	})
	add(m, "hassuffix", strings.HasSuffix)
	add(m, "trimsuffix", strings.TrimSuffix)
	add(m, "splitlines", func(s string) []string {
		return strings.Split(s, "\n")
	})
	add(m, "slice", func(s string, start, end int) string {
		return s[start:end]
	})
	add(m, "sub", func(a, b int) int { return a - b })
	add(m, "textwrap", textwrap.String)
	if tfa, ok := mc.(TemplateFuncsAdder); ok {
		tfa.AddFuncs(m)
	}
	return t
}
