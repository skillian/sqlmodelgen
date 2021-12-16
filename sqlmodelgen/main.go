package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/template"

	"github.com/davecgh/go-spew/spew"
	_ "github.com/denisenkom/go-mssqldb"

	"github.com/skillian/argparse"
	"github.com/skillian/expr/errors"
	"github.com/skillian/expr/stream/sqlstream"
	"github.com/skillian/logging"
	"github.com/skillian/sqlmodelgen"
)

var (
	logger = logging.GetLogger(
		"sqlmodelgen",
		logging.LoggerHandler(
			new(logging.ConsoleHandler),
			logging.HandlerFormatter(logging.DefaultFormatter{}),
			logging.HandlerLevel(logging.VerboseLevel),
		),
	)
)

type Args struct {
	LogLevel               logging.Level
	ConfigFile             string
	GeneratorModelContexts []ArgModelContext
	TemplateModelContexts  []ArgModelContext
	TemplateDir            string
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
				"\tverbose:  Most detailed logging.  Usually "+
				"for troubleshooting tricky issues.\n"+
				"\tdebug:    Slightly less verbose than "+
				"\"verbose\", but still detailed.\n"+
				"\tinfo:     Show informational messages that "+
				"explain why a decision was made, but no "+
				"action is necessary.\n"+
				"\twarn:     Show warning messages for errors "+
				"that were handled but might be a sign of "+
				"something that has to be addressed "+
				"(Default).\n"+
				"\terror:    Only show unhandled errors.",
		),
	).MustBind(&args.LogLevel)
	parser.MustAddArgument(
		argparse.OptionStrings("-g", "--generator"),
		argparse.MetaVar("TYPE", "OUTPUT_FILE"),
		argparse.ActionFunc(generatorAction{}),
		argparse.Nargs(2),
		argparse.Help(
			"Generate a JSON model which can be consumed by a "+
				"template generator.  Supported options are:\n"+
				"\tsql-reflector:  Generate a model from a "+
				"SQL database\n"+
				"\tdrawio:         Use a draw.io/"+
				"diagrams.net ERD diagram",
		),
	).MustBind(&args.GeneratorModelContexts)
	parser.MustAddArgument(
		argparse.OptionStrings("-t", "--template"),
		argparse.MetaVar("TYPE", "NAMESPACE", "OUTPUT_FILE"),
		argparse.ActionFunc(templateAction{}),
		argparse.Nargs(3),
		argparse.Help(
			"Generate templated output from the model.  "+
				"Supported options are:\n"+
				"\tgo-sql:     Go SQL models\n"+
				"\tgo-models:  Go data models\n"+
				"\tcs:         C# SQL models\n"+
				"\twvace:      Hyland OnBase WorkView ACE "+
				"file\n",
		),
	).MustBind(&args.TemplateModelContexts)

	// TODO: Handle GeneratorModelContexts AND/OR TemplateModelContexts.

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
	configReader, err := os.Open(args.ConfigFile)
	if err != nil {
		return errors.Errorf1From(
			err, "failed to open config file %q",
			args.ConfigFile,
		)
	}
	defer errors.Catch(&Err, configReader.Close)
	var mm *sqlstream.MetaModel
	// getMetaModel lazily loads the configuration file and re-uses it
	getMetaModel := func(r io.Reader) (mm2 *sqlstream.MetaModel, err error) {
		if mm != nil {
			return mm, nil
		}
		mm2, err = sqlmodelgen.MetaModelFromJSON(r)
		if err != nil {
			return nil, errors.Errorf1From(
				err, "failed to load model from %v",
				r,
			)
		}
		if logger.EffectiveLevel() <= logging.VerboseLevel {
			logger.Verbose("configuration:\n\n%v", spew.Sdump(mm))
		}
		mm = mm2
		return
	}
	for _, amcs := range [][]ArgModelContext{args.GeneratorModelContexts, args.TemplateModelContexts} {
		for _, amc := range amcs {
			var out io.WriteCloser
			var err error
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
			switch mc := amc.ModelContext.(type) {
			case sqlmodelgen.ModelConfigParser:
				cfg, err := mc.ParseModelConfig(context.TODO(), configReader)
				if err != nil {
					return errors.Errorf1From(
						err, "failed to parse model configuration from %v",
						configReader,
					)
				}
				// overwrite getMetaModel to use the model we just
				// parsed from the configuration.  Note that now the
				// io.Reader is ignored.
				getMetaModel = func(r io.Reader) (mm2 *sqlstream.MetaModel, err error) {
					if mm != nil {
						return mm, nil
					}
					mm2, err = sqlmodelgen.MetaModelFromConfig(cfg)
					if err != nil {
						return nil, errors.Errorf2From(
							err, "failed to create %T from %v",
							mm2, cfg,
						)
					}
					mm = mm2
					return
				}
				switch mc := mc.(type) {
				case sqlmodelgen.ModelConfigWriter:
					err = mc.WriteModelConfig(out, cfg)
					if err != nil {
						return errors.Errorf2From(
							err, "failed to write "+
								"configuration %v to %v",
							cfg, out,
						)
					}
				case sqlmodelgen.MetaModelWriter:
					mm, err = getMetaModel(nil) // parameter is ignored here.
					if err != nil {
						return err
					}
					err = mc.WriteMetaModel(out, mm)
					if err != nil {
						return errors.Errorf2From(
							err, "failed to write %v to %v",
							mm, out,
						)
					}
				default:
					// emit JSON.
					bs, err := json.Marshal(cfg)
					if err != nil {
						return errors.Errorf0From(
							err, "error serializing configuration into JSON",
						)
					}
					if _, err = io.Copy(out, bytes.NewReader(bs)); err != nil {
						return errors.Errorf1From(
							err, "failed to write JSON to %v", out,
						)
					}
				}

			case sqlmodelgen.TemplateContext:
				if mm, err = getMetaModel(configReader); err != nil {
					return err
				}
				td, err := sqlmodelgen.TemplateDataFromMetaModel(mm, amc.ModelContext)
				if err != nil {
					return err
				}
				td.Namespace = amc.Args[0]
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
				if err = t.ExecuteTemplate(out, "0root.txt", td); err != nil {
					return errors.Errorf1From(
						err, "error executing template: %v", t,
					)
				}

			case sqlmodelgen.MetaModelWriter:
				if mm, err = getMetaModel(configReader); err != nil {
					return err
				}
				if err = mc.WriteMetaModel(out, mm); err != nil {
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
	}
	return nil
}

type nopWriteCloser struct{ io.Writer }

func (n nopWriteCloser) Close() error { return nil }

type ignoreArg struct{}

type templateAction struct{}

var _ argparse.ArgumentAction = templateAction{}

var templateChoices = []argparse.Choice{
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
	// {
	// 	Key:   "sql-reflect",
	// 	Value: sqlmodelgen.SQLStreamReflectorModelContext,
	// },
	// {
	// 	Key:   "drawio",
	// 	Value: sqlmodelgen.DrawIOModelContext,
	// },
	{
		Key:   "wvace",
		Value: sqlmodelgen.WVAceModelContext,
	},
}

type ArgModelContext struct {
	ModelContext sqlmodelgen.ModelContext
	ModelFile    string

	// Args holds additional arguments to the ModelContext argument.
	// For example, for templates it contains the namespace to use in the
	// template.
	Args []string
}

func (t templateAction) Name() string { return "Template action" }
func (t templateAction) UpdateNamespace(a *argparse.Argument, ns argparse.Namespace, vs []interface{}) error {
	const expectNargs = 3
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
	for _, c := range templateChoices {
		if c.Key != s {
			continue
		}
		handledKey = true
		amc := ArgModelContext{
			ModelContext: c.Value.(sqlmodelgen.ModelContext),
		}
		if s, ok = vs[1].(string); !ok {
			s = fmt.Sprint(vs[1])
		}
		amc.ModelFile = s
		if s, ok = vs[2].(string); !ok {
			s = fmt.Sprint(vs[1])
		}
		amc.Args = []string{s}
		ns.Append(a, amc)
		break
	}
	if !handledKey {
		return errors.Errorf1("unknown type choice: %q", s)
	}
	return nil
}

type generatorAction struct{}

var _ argparse.ArgumentAction = generatorAction{}

var generatorChoices = []argparse.Choice{
	{
		Key:   "sql-reflect",
		Value: sqlmodelgen.SQLStreamReflectorModelContext,
	},
	{
		Key:   "drawio",
		Value: sqlmodelgen.DrawIOModelContext,
	},
}

func (g generatorAction) Name() string { return "Generator action" }
func (g generatorAction) UpdateNamespace(a *argparse.Argument, ns argparse.Namespace, vs []interface{}) error {
	const expectNargs = 2
	if len(vs) != expectNargs {
		return errors.Errorf3(
			"%v expected %d arguments, not %d",
			g.Name(), expectNargs, len(vs),
		)
	}
	s, ok := vs[0].(string)
	if !ok {
		s = fmt.Sprint(vs[0])
	}
	handledKey := false
	for _, c := range generatorChoices {
		if c.Key != s {
			continue
		}
		handledKey = true
		amc := ArgModelContext{
			ModelContext: c.Value.(sqlmodelgen.ModelContext),
		}
		if s, ok = vs[1].(string); !ok {
			s = fmt.Sprint(vs[1])
		}
		amc.ModelFile = s
		ns.Append(a, amc)
		break
	}
	if !handledKey {
		return errors.Errorf1("unknown type choice: %q", s)
	}
	return nil
}
