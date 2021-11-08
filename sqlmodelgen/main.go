package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"text/template"

	"github.com/davecgh/go-spew/spew"
	_ "github.com/denisenkom/go-mssqldb"

	"github.com/skillian/argparse"
	"github.com/skillian/expr"
	"github.com/skillian/expr/errors"
	"github.com/skillian/expr/stream/sqlstream"
	"github.com/skillian/logging"
	"github.com/skillian/sqlmodelgen"
)

var (
	logger = logging.GetLogger(
		"expr",
		logging.LoggerHandler(
			new(logging.ConsoleHandler),
			logging.HandlerFormatter(logging.DefaultFormatter{}),
			logging.HandlerLevel(logging.VerboseLevel),
		),
	)
)

type Args struct {
	LogLevel      logging.Level
	ConfigFile    string
	ModelContexts []ArgModelContext
	TemplateDir   string
}

func main() {
	var args Args
	parser := argparse.MustNewArgumentParser(
		argparse.Description(
			"Generate models from SQL definitions",
		),
	)
	parser.MustAddArgument(
		argparse.OptionStrings("--log-level"),
		argparse.Action("store"),
		argparse.Choices(
			argparse.Choice{Key: "verbose", Value: logging.VerboseLevel},
			argparse.Choice{Key: "debug", Value: logging.DebugLevel},
			argparse.Choice{Key: "info", Value: logging.InfoLevel},
			argparse.Choice{Key: "warn", Value: logging.WarnLevel},
			argparse.Choice{Key: "error", Value: logging.ErrorLevel},
		),
		argparse.Default("warn"),
		argparse.Help(
			"Specify the logging level.  Options include:\n\n"+
				"\tverbose:\tMost detailed logging.  Usually "+
				"for troubleshooting tricky issues.\n"+
				"\tdebug:\tSlightly less verbose than "+
				"\"verbose\", but still detailed.\n"+
				"\tinfo:\tShow informational messages that "+
				"explain why a decision was made, but no "+
				"action is necessary.\n"+
				"\twarn:\tShow warning messages for errors "+
				"that were handled but might be a sign of "+
				"something that has to be addressed "+
				"(Default).\n"+
				"\terror:\tOnly show unhandled errors.",
		),
	).MustBind(&args.LogLevel)
	parser.MustAddArgument(
		argparse.OptionStrings("-t", "--type"),
		argparse.ActionFunc(typeAction{}),
		argparse.Nargs(2),
		argparse.Help(
			"Specify the type of output to generate.\n\n"+
				"Options:\n\n"+
				"\tcs:\tC# models\n"+
				"\tgo-sql:\tGo SQL models\n"+
				"\tgo-models:\tGo entity models\n"+
				"\tjson:\tGenerate JSON from an existing "+
				"database schema.\n"+
				"\twvace:\tGenerate a Hyland OnBase WorkView "+
				"ACE (Application Creation \"Excelerator\") "+
				"file\n\n"+
				"And then the output filename.",
		),
	).MustBind(&args.ModelContexts)
	parser.MustAddArgument(
		argparse.OptionStrings("-T", "--template-dir"),
		argparse.Action("store"),
		argparse.Default(""),
		argparse.Help(
			"Optional custom template directory to override the "+
				"integrated templates",
		),
	).MustBind(&args.TemplateDir)
	parser.MustAddArgument(
		argparse.Dest("configfile"),
		argparse.Action("store"),
		argparse.Help(
			"configuration file from which the model is "+
				"derived except for the \"json\" output type "+
				"which expects a connection configuration file",
		),
	).MustBind(&args.ConfigFile)
	parser.MustParseArgs()

	if err := Main(args); err != nil {
		panic(errors.WithoutParentStackTrace(err))
	}
}

func Main(args Args) (Err error) {
	logger.SetLevel(args.LogLevel)
	f, err := os.Open(args.ConfigFile)
	if err != nil {
		return errors.Errorf1From(
			err, "failed to open config file %q",
			args.ConfigFile,
		)
	}
	defer errors.Catch(&Err, f.Close)
	for _, amc := range args.ModelContexts {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return errors.Errorf1From(
				err, "failed to seek to beginning of file %v",
				args.ConfigFile,
			)
		}
		if amc.ModelContext == sqlmodelgen.JSONModelContext {
			if err := EmitJSONModel(amc, f); err != nil {
				return err
			}
			continue
		}
		cfg, err := sqlmodelgen.ConfigFromJSON(f, amc.ModelContext)
		if err != nil {
			return errors.Errorf1From(
				err, "failed to parse file %v as JSON",
				args.ConfigFile,
			)
		}
		var out io.WriteCloser
		if amc.ModelFile == "" {
			out = nopWriteCloser{os.Stdout}
		} else {
			out, err = os.Create(amc.ModelFile)
			if err != nil {
				return errors.Errorf1From(
					err, "failed to create output file: %v",
					amc.ModelFile,
				)
			}
		}
		if logger.Level() <= logging.VerboseLevel {
			logger.Verbose("configuration:\n\n%v", spew.Sdump(cfg))
		}
		switch mc := amc.ModelContext.(type) {
		case sqlmodelgen.TemplateContext:
			fm := make(template.FuncMap, 8)
			t := sqlmodelgen.AddFuncs(
				template.New("<sqlmodelgen>"), fm, amc.ModelContext,
			).Funcs(fm)
			if args.TemplateDir == "" {
				fsys := mc.FS()
				t, err = t.ParseFS(fsys, "*.txt")
				if err != nil {
					return errors.Errorf1From(
						err, "failed to parse ModelContext file "+
							"system: %v",
						fsys,
					)
				}
			} else {
				t, err = t.ParseFiles(args.TemplateDir, "*.txt")
				if err != nil {
					return errors.Errorf1From(
						err, "failed to parse template directory: %v",
						args.TemplateDir,
					)
				}
			}
			if err = t.ExecuteTemplate(out, "0root.txt", cfg); err != nil {
				return errors.Errorf1From(
					err, "error executing template: %v", t,
				)
			}

		case sqlmodelgen.ModelWriter:
			if err = mc.WriteModel(out, cfg); err != nil {
				return errors.Errorf1From(
					err, "error executing model writer: %[1]v "+
						"(type: %[1]T)",
					mc,
				)
			}

		default:
			return errors.Errorf1(
				"Unknown model context %[1]v (type: %[1]T)",
				amc.ModelContext,
			)
		}
		if err := out.Close(); err != nil {
			return errors.Errorf1From(
				err, "error attempting to close %v", amc.ModelFile,
			)
		}
	}
	return nil
}

func EmitJSONModel(args ArgModelContext, r io.Reader) (Err error) {
	type ConnectionConfig struct {
		DriverName       string
		ConnectionString string
		DialectName      string
		ReflectorName    string
	}
	mcw, ok := args.ModelContext.(sqlmodelgen.ModelConfigWriter)
	if !ok {
		return errors.Errorf(
			"expected a model config writer, but got %[1]v "+
				"(type: %[1]T)",
			args.ModelContext,
		)
	}
	w, err := os.Create(args.ModelFile)
	if err != nil {
		return errors.Errorf1From(
			err, "failed to open output file %v", args.ModelFile,
		)
	}
	defer errors.Catch(&Err, w.Close)
	c := ConnectionConfig{}
	{
		bs, err := ioutil.ReadAll(r)
		if err != nil {
			return errors.Errorf1From(
				err, "failed to read all data from %v", r,
			)
		}
		if err = json.Unmarshal(bs, &c); err != nil {
			return errors.Errorf0From(
				err, "failed to unmarshal connection "+
					"configuration JSON",
			)
		}
	}
	d, err := sqlstream.ParseDialect(c.DialectName)
	if err != nil {
		return errors.Errorf1From(
			err, "error parsing dialect: %q", c.DialectName,
		)
	}
	re, err := sqlstream.ParseReflector(c.ReflectorName)
	if err != nil {
		return errors.Errorf1From(
			err, "error parsing reflector: %q", c.ReflectorName,
		)
	}
	sqlDB, err := sql.Open(c.DriverName, c.ConnectionString)
	if err != nil {
		return errors.Errorf0From(
			err, "failed to establish SQL connection",
		)
	}
	db, err := sqlstream.NewDB(sqlDB, sqlstream.WithDialect(d))
	if err != nil {
		return errors.Errorf2From(
			err, "error creating sqlstream database from %v, "+
				"with dialect %v",
			sqlDB, d,
		)
	}
	ctx, _ := expr.ValuesFromContextOrNew(context.Background())
	cfg, err := re.Config(ctx, db)
	if err != nil {
		return errors.Errorf1From(
			err, "failed to create configuration from database %v",
			db,
		)
	}
	if err = mcw.WriteModelConfig(w, cfg); err != nil {
		return errors.Errorf2From(
			err, "failed to write model config %v out to %v",
			cfg, args.ModelFile,
		)
	}
	return nil
}

type nopWriteCloser struct{ io.Writer }

func (n nopWriteCloser) Close() error { return nil }

type typeAction struct{}

var _ argparse.ArgumentAction = typeAction{}

var typeChoices = []argparse.Choice{
	{
		Key:   "cs",
		Value: sqlmodelgen.CSModelContext,
	},
	{
		Key:   "go-sql",
		Value: sqlmodelgen.GoSQLModelContext,
	},
	{
		Key:   "go-models",
		Value: sqlmodelgen.GoModelsModelContext,
	},
	{
		Key:   "json",
		Value: sqlmodelgen.JSONModelContext,
	},
	{
		Key:   "wvace",
		Value: sqlmodelgen.WVAceModelContext,
	},
}

type ArgModelContext struct {
	ModelContext sqlmodelgen.ModelContext
	ModelFile    string
}

func (t typeAction) Name() string { return "Type action" }
func (t typeAction) UpdateNamespace(a *argparse.Argument, ns argparse.Namespace, vs []interface{}) error {
	const expectNargs = 2
	if len(vs) != expectNargs {
		return errors.Errorf3(
			"%v expected %d arguments, not %d",
			t.Name(), expectNargs, len(vs),
		)
	}
	s, ok := vs[0].(string)
	if !ok {
		s = fmt.Sprint(vs[0])
	}
	handledKey := false
	for _, c := range typeChoices {
		if c.Key != s {
			continue
		}
		handledKey = true
		if s, ok = vs[1].(string); !ok {
			s = fmt.Sprint(vs[1])
		}
		ns.Append(a, ArgModelContext{
			ModelContext: c.Value.(sqlmodelgen.ModelContext),
			ModelFile:    s,
		})
		break
	}
	if !handledKey {
		return errors.Errorf1("unknown type choice: %q", s)
	}
	return nil
}
