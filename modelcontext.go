package sqlmodelgen

import (
	"context"
	"io"
	"io/fs"
	"text/template"

	"github.com/skillian/expr/errors"
	"github.com/skillian/expr/stream/sqlstream"
	"github.com/skillian/expr/stream/sqlstream/config"
	"github.com/skillian/expr/stream/sqlstream/sqltypes"
)

// ModelContext is the base interface that defines how output should be
// produced for a given model.
type ModelContext interface {
	// ModelType translates a sqltypes.Type into a data type.
	ModelType(t sqltypes.Type) (namespace, typename string, err error)
}

// TemplateContext is implemented by ModelContexts that produce their output
// via text/template.
type TemplateContext interface {
	// FS returns the directory of templates that should be used
	// unless overridden by a command line parameter
	FS() fs.FS
}

// TemplateFuncsAdder adds functions to the template FuncMap before
// executing the templates defined in the TemplateContext's FS.
type TemplateFuncsAdder interface {
	AddFuncs(m template.FuncMap)
}

// NamespaceEnsurer is an optional interface that ModelContexts can implement
// to inspect the initialized configuration and return namespaces that must
// exist in the generated templates.
type NamespaceEnsurer interface {
	EnsureNamespaces(c *sqlstream.MetaModel) []string
}

// NamespaceOrganizer is an optional interface that ModelContexts can implement
// to organize the namespaces of the files they generate (e.g. sort them,
// group them, etc.)
type NamespaceOrganizer interface {
	// OrganizeNamespaces receives an unordered collection of namespaces
	// and must return the order of the namespaces as they should appear
	// in the output file.  Blank namespaces can be inserted to create
	// gaps (newlines, for most models) in the namespaces.
	OrganizeNamespaces(ns []string) []string
}

// TemplateData combines a MetaModel and namespaces to be included at the
// top of the template(s) being emitted.
type TemplateData struct {
	Namespace  string
	Namespaces []string
	Parameters map[string]string
	*sqlstream.MetaModel
}

// TemplateDataFromMetaModel initializes TemplateData from a MetaModel and
// a ModelContext.
func TemplateDataFromMetaModel(mm *sqlstream.MetaModel, mc ModelContext) (TemplateData, error) {
	// TODO: I don't like this function here.  Maybe it should go into
	// the main package?  It seems like it just does some helper work with
	// the namespaces.  Maybe it just needs a better name?
	nss := make(map[string]struct{}, 8)
	for _, db := range mm.Databases {
		for _, sch := range db.Schemas {
			for _, tbl := range sch.Tables {
				for _, col := range tbl.Columns {
					ns, _, err := mc.ModelType(col.Type)
					if err != nil {
						return TemplateData{}, errors.ErrorfFrom(
							err, "failed to get model type of column: %v.%v.%v.%v",
							db.RawName, sch.RawName, tbl.RawName, col.RawName,
						)
					}
					nss[ns] = struct{}{}
				}
			}
		}
	}
	td := TemplateData{
		MetaModel:  mm,
		Parameters: make(map[string]string, 4),
	}
	if ens, ok := mc.(NamespaceEnsurer); ok {
		for _, ns := range ens.EnsureNamespaces(mm) {
			nss[ns] = struct{}{}
		}
	}
	td.Namespaces = make([]string, 0, len(nss))
	for ns := range nss {
		if ns == "" {
			continue
		}
		td.Namespaces = append(td.Namespaces, ns)
	}
	if org, ok := mc.(NamespaceOrganizer); ok {
		td.Namespaces = org.OrganizeNamespaces(td.Namespaces)
	}
	return td, nil
}

// ModelWriter can be implemented instead of TemplateContext to write arbitrary
// output right into an output file.
type MetaModelWriter interface {
	WriteMetaModel(w io.Writer, c *sqlstream.MetaModel) error
}

type TemplateDataWriter interface {
	WriteTemplateData(w io.Writer, td TemplateData) error
}

// ModelConfigWriter is like MetaModelWriter but can write the "unlinked"
// config.Config instead of requiring the fully parsed and linked
// sqlstream.Config.
type ModelConfigWriter interface {
	WriteModelConfig(w io.Writer, c config.Config) error
}

// ModelConfigParser reads some optional configuration from r and parses
// it into a model configuration.
type ModelConfigParser interface {
	ParseModelConfig(ctx context.Context, r io.Reader) (config.Config, error)
}
