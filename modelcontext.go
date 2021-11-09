package sqlmodelgen

import (
	"io"
	"io/fs"
	"text/template"

	//"github.com/skillian/expr/errors"
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

type TemplateFuncsAdder interface {
	AddFuncs(m template.FuncMap)
}

// NamespaceEnsurer is an optional interface that ModelContexts can implement
// to inspect the initialized configuration and return namespaces that must
// exist in the generated templates.
type NamespaceEnsurer interface {
	EnsureNamespaces(c *sqlstream.Config) []string
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

// ModelWriter can be implemented instead of TemplateContext to write arbitrary
// output right into an output file.
type ModelWriter interface {
	WriteModel(w io.Writer, c *sqlstream.Config) error
}

// ModelConfigWriter is like ModelWriter but can write the "unlinked"
// config.Config instead of requiring the fully parsed and linked
// sqlstream.Config.
type ModelConfigWriter interface {
	WriteModelConfig(w io.Writer, c config.Config) error
}
