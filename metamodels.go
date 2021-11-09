package sqlmodelgen

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"strings"

	"github.com/skillian/expr/errors"
	"github.com/skillian/expr/stream/sqlstream"
	"github.com/skillian/expr/stream/sqlstream/config"
	"github.com/skillian/expr/stream/sqlstream/sqltypes"
)

// ConfigFromJSON reads JSON data from the reader, r, and
// instantiates a Config model from it.
func ConfigFromJSON(r io.Reader, mc ModelContext) (*sqlstream.Config, error) {
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, errors.Errorf1From(
			err, "failed to load JSON from %v", r)
	}
	var j config.Config
	if err = json.Unmarshal(data, &j); err != nil {
		return nil, errors.Errorf1From(
			err, "failed to parse %q as JSON", string(data))
	}
	c := &sqlstream.Config{}
	if err = (&configBuilder{Config: c, ModelContext: mc}).init(&j); err != nil {
		js, err2 := json.MarshalIndent(j, "", "\t")
		if err2 != nil {
			js = []byte("(error!)")
			err = errors.Aggregate(
				errors.Errorf1From(
					err2, "failed to marshal %v into JSON",
					data,
				),
				err,
			)
		}
		return nil, errors.Errorf1From(
			err, "failed to initialize configuration "+
				"from JSON:\n\n%q", string(js))
	}
	return c, nil
}

type configBuilder struct {
	*sqlstream.Config
	ModelContext
	namespaces map[string]struct{}
	caches     struct {
		columns   []sqlstream.Column
		tables    []sqlstream.Table
		schemas   []sqlstream.Schema
		databases []sqlstream.Database
		ids       []sqlstream.TableID
		keys      []sqlstream.TableKey
		keyIDs    []*sqlstream.TableID
	}
}

func (b *configBuilder) init(c *config.Config) (err error) {
	b.namespaces = make(map[string]struct{}, 8)
	tempIDs := make([]*sqlstream.TableID, 0, 16)
	if err = b.Config.DatabaseNamers.Init(&c.DatabaseNamers); err != nil {
		return
	}
	b.Config.Namespace = c.Namespace
	b.Config.Databases = make([]*sqlstream.Database, 0, len(c.Databases))
	b.Config.DatabasesByName = make(map[string]*sqlstream.Database, len(c.Databases))
	for _, dbCfg := range c.Databases {
		d, dbErr := b.newDatabase(&dbCfg)
		if dbErr != nil {
			return errors.Errorf1From(
				dbErr, "failed to initialize "+
					"database: %q", dbCfg.RawName)
		}
		b.Databases = append(b.Databases, d)
		b.DatabasesByName[dbCfg.RawName] = d
		for _, schCfg := range dbCfg.Schemas {
			s := b.newSchema(d, &schCfg)
			d.Schemas = append(d.Schemas, s)
			d.SchemasByName[schCfg.RawName] = s
			for _, tblCfg := range schCfg.Tables {
				t := b.newTable(s, &tblCfg)
				s.Tables = append(s.Tables, t)
				s.TablesByName[tblCfg.RawName] = t
				for _, colCfg := range tblCfg.Columns {
					c := b.newColumn(t, &colCfg)
					t.Columns = append(t.Columns, c)
					t.ColumnsByName[colCfg.RawName] = c
					c.PK = colCfg.PK
					if colCfg.Type != "" {
						c.Type, err = sqltypes.Parse(colCfg.Type)
						if err != nil {
							return errors.ErrorfFrom(
								err,
								"column %s.%s.%s.%s has an invalid Type",
								dbCfg.RawName, schCfg.RawName,
								tblCfg.RawName, colCfg.RawName,
							)
						}
						ns, _, err := b.ModelType(c.Type)
						if err != nil {
							return errors.ErrorfFrom(
								err,
								"failed to determine "+
									"model type of "+
									"column "+
									"%s.%s.%s.%s",
								dbCfg.RawName, schCfg.RawName,
								tblCfg.RawName, colCfg.RawName,
							)
						}
						if len(ns) > 0 {
							b.namespaces[ns] = struct{}{}
						}
					}
					if c.PK {
						id := b.newID(c, &colCfg)
						tempIDs = append(tempIDs, id)
					}
				}
				if len(tempIDs) > 0 {
					switch len(tempIDs) {
					case 1:
						t.PK = tempIDs[0]
					default:
						t.Key = b.newKey(t, tempIDs)
					}
					tempIDs = tempIDs[:0]
				}
			}
		}
	}
	// Link up the FKs...
	if err = b.iterDBSchemaTableColumn(c, func(x dbSchemaTableColumn) error {
		if x.colCfg.FK == "" {
			return nil
		}
		fkTrg, err := b.getPathUp(x.colCfg.FK, x.table)
		if err != nil {
			return errors.ErrorfFrom(
				err, "failed to initialize column %v.%v.%v.%v FK",
				x.dbName, x.schName, x.tblName, x.colName,
			)
		}
		fkCol, ok := fkTrg.(*sqlstream.Column)
		if !ok {
			return errors.Errorf(
				"column %v.%v.%v.%v FK target is not a column",
				x.dbName, x.schName, x.tblName, x.colName,
			)
		}
		fkTbl := fkCol.Table
		if fkTbl.PK != nil && fkTbl.PK.Column == fkCol {
			x.column.FK = fkTbl.PK
			fkCol.FKCols = append(fkCol.FKCols, x.column)
			if x.column.Type == nil {
				x.column.Type = fkTbl.PK.Column.Type
			}
		} else if fkTbl.Key != nil {
			for _, id := range fkTbl.Key.IDs {
				if id.Column == fkCol {
					x.column.FK = id
					id.Column.FKCols = append(id.Column.FKCols, x.column)
					if x.column.Type == nil {
						x.column.Type = id.Column.Type
					}
					break
				}
			}
		}
		if x.column.FK == nil {
			return errors.Errorf(
				"column %q is not key within primary table %q",
				fkCol.RawName, fkCol.Table.RawName)
		}
		return nil
	}); err != nil {
		return err
	}
	// Create the DataColumns list for non FKs and PKs...
	if err = b.iterDBSchemaTableColumn(c, func(x dbSchemaTableColumn) error {
		if x.column.PK {
			return nil
		}
		if x.column.FK != nil {
			return nil
		}
		if x.table.Key != nil {
			for _, id := range x.table.Key.IDs {
				if id.Column == x.column {
					return nil
				}
			}
		}
		x.table.DataColumns = append(x.table.DataColumns, x.column)
		return nil
	}); err != nil {
		return err
	}
	if ens, ok := b.ModelContext.(NamespaceEnsurer); ok {
		for _, ns := range ens.EnsureNamespaces(b.Config) {
			if ns == "" {
				continue
			}
			b.namespaces[ns] = struct{}{}
		}
	}
	b.Config.Namespaces = make([]string, 0, len(b.namespaces))
	for ns := range b.namespaces {
		b.Config.Namespaces = append(b.Config.Namespaces, ns)
	}
	if org, ok := b.ModelContext.(NamespaceOrganizer); ok {
		b.Config.Namespaces = org.OrganizeNamespaces(b.Config.Namespaces)
	}
	return
}

type dbSchemaTableColumn struct {
	dbName  string
	dbCfg   config.Database
	db      *sqlstream.Database
	schName string
	schCfg  config.Schema
	schema  *sqlstream.Schema
	tblName string
	tblCfg  config.Table
	table   *sqlstream.Table
	colName string
	colCfg  config.Column
	column  *sqlstream.Column
}

func (b *configBuilder) iterDBSchemaTableColumn(c *config.Config, f func(dbSchemaTableColumn) error) error {
	for _, dbCfg := range c.Databases {
		db := b.Config.DatabasesByName[dbCfg.RawName]
		for _, schCfg := range dbCfg.Schemas {
			schema := db.SchemasByName[schCfg.RawName]
			for _, tblCfg := range schCfg.Tables {
				table := schema.TablesByName[tblCfg.RawName]
				for _, colCfg := range tblCfg.Columns {
					column := table.ColumnsByName[colCfg.RawName]
					if err := f(dbSchemaTableColumn{
						dbCfg.RawName, dbCfg, db,
						schCfg.RawName, schCfg, schema,
						tblCfg.RawName, tblCfg, table,
						colCfg.RawName, colCfg, column,
					}); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (b *configBuilder) getPathUp(path string, start *sqlstream.Table) (interface{}, error) {
	hops := strings.Count(path, ".")
	var root interface{}
	switch hops {
	case 0:
		root = start
	case 1:
		root = start.Schema
	case 2:
		root = start.Schema.Database
	case 3:
		root = start.Schema.Database.Config
	default:
		return nil, errors.Errorf("%q does not seem to be a path")
	}
	return b.getPathDown(path, root)
}

func (b *configBuilder) getPathDown(path string, start interface{}) (interface{}, error) {
	parts := strings.Split(path, ".")
	hop := start
	var ok bool
	for len(parts) > 0 {
		last := hop
		name := parts[0]
		parts = parts[1:]
		switch x := hop.(type) {
		case *sqlstream.Config:
			hop, ok = x.DatabasesByName[name]
		case *sqlstream.Database:
			hop, ok = x.SchemasByName[name]
		case *sqlstream.Schema:
			hop, ok = x.TablesByName[name]
		case *sqlstream.Table:
			hop, ok = x.ColumnsByName[name]
		default:
			return nil, errors.Errorf(
				"cannot get %[1]q from %[2]v "+
					"(type: %[2]T):  %[3]v "+
					"(type: %[3]T) has no %[4]q "+
					"member",
				path, last, hop, name)
		}
		if !ok {
			return nil, errors.Errorf(
				"failed to get %[1]q from %[2]T %[2]v",
				name, last,
			)
		}
	}
	return hop, nil
}

func (b *configBuilder) newColumn(t *sqlstream.Table, cfg *config.Column) (c *sqlstream.Column) {
	if len(b.caches.columns) == cap(b.caches.columns) {
		b.caches.columns = make([]sqlstream.Column, 1024)
	}
	c = &b.caches.columns[0]
	b.caches.columns = b.caches.columns[1:]
	c.Doc = cfg.Doc
	c.Table = t
	c.Names.InitFromConfig(cfg.Names, &t.Database.Namers.Column)
	return
}

func (b *configBuilder) newIDs(ids []*sqlstream.TableID) (keyIDs []*sqlstream.TableID) {
	const defaultKeyIDCap = 16
	if len(b.caches.keyIDs)+len(ids) > cap(b.caches.keyIDs) {
		if len(ids) > defaultKeyIDCap {
			keyIDs = make([]*sqlstream.TableID, len(ids))
			copy(keyIDs, ids)
			return
		}
		b.caches.keyIDs = make([]*sqlstream.TableID, defaultKeyIDCap)
	}
	keyIDs = b.caches.keyIDs[:len(ids):len(ids)]
	copy(keyIDs, ids)
	return
}

func (b *configBuilder) newID(c *sqlstream.Column, cfg *config.Column) (id *sqlstream.TableID) {
	if len(b.caches.ids) == cap(b.caches.ids) {
		b.caches.ids = make([]sqlstream.TableID, 128)
	}
	id = &b.caches.ids[0]
	b.caches.ids = b.caches.ids[1:]
	id.Doc = cfg.Doc
	id.Column = c
	temp := c.Names
	temp.RawName = strings.TrimSuffix(temp.RawName, " id")
	id.Names.Init(temp, &c.Table.Database.Namers.ID)
	return
}

func (b *configBuilder) newKey(t *sqlstream.Table, ids []*sqlstream.TableID) (key *sqlstream.TableKey) {
	if len(b.caches.keys) == cap(b.caches.keys) {
		b.caches.keys = make([]sqlstream.TableKey, 16)
	}
	key = &b.caches.keys[0]
	b.caches.keys = b.caches.keys[1:]
	ns := t.Names
	ns.RawName += " key"
	if ns.SQLName != "" {
		ns.SQLName += "Key"
	}
	if ns.ModelName != "" {
		ns.ModelName += "Key"
	}
	key.Names.Init(ns, &t.Database.Namers.Key)
	key.IDs = b.newIDs(ids)
	return
}

func (b *configBuilder) newTable(s *sqlstream.Schema, c *config.Table) (t *sqlstream.Table) {
	if len(b.caches.tables) == cap(b.caches.tables) {
		b.caches.tables = make([]sqlstream.Table, 128)
	}
	t = &b.caches.tables[0]
	b.caches.tables = b.caches.tables[1:]
	t.Doc = c.Doc
	t.Schema = s
	t.Names.InitFromConfig(c.Names, &s.Database.Namers.Table)
	t.Columns = make([]*sqlstream.Column, 0, len(c.Columns))
	t.ColumnsByName = make(map[string]*sqlstream.Column, len(c.Columns))
	return
}

func (b *configBuilder) newSchema(d *sqlstream.Database, c *config.Schema) (s *sqlstream.Schema) {
	if len(b.caches.schemas) == cap(b.caches.schemas) {
		b.caches.schemas = make([]sqlstream.Schema, 8)
	}
	s = &b.caches.schemas[0]
	b.caches.schemas = b.caches.schemas[1:]
	s.Doc = c.Doc
	s.Database = d
	s.Names.InitFromConfig(c.Names, &d.Namers.Schema)
	s.Tables = make([]*sqlstream.Table, 0, len(c.Tables))
	s.TablesByName = make(map[string]*sqlstream.Table, len(c.Tables))
	return
}

func (b *configBuilder) newDatabase(c *config.Database) (d *sqlstream.Database, err error) {
	if len(b.caches.databases) == cap(b.caches.databases) {
		b.caches.databases = make([]sqlstream.Database, 4)
	}
	d = &b.caches.databases[0]
	b.caches.databases = b.caches.databases[1:]
	d.Doc = c.Doc
	d.Config = b.Config
	d.Names.InitFromConfig(c.CommonData.Names, &b.DatabaseNamers)
	d.Schemas = make([]*sqlstream.Schema, 0, len(c.Schemas))
	d.SchemasByName = make(map[string]*sqlstream.Schema, len(c.Schemas))
	initNamers := func(ofWhat string, nrs *sqlstream.Namers, c *config.Namers) (err error) {
		if err = nrs.Init(c); err != nil {
			return errors.Errorf1From(
				err, "failed to initialize %s", ofWhat)
		}
		return
	}
	if err = initNamers("column", &d.Namers.Column, &c.Namers.Column); err != nil {
		return
	}
	if err = initNamers("id", &d.Namers.ID, &c.Namers.IDType); err != nil {
		return
	}
	if err = initNamers("key", &d.Namers.Key, &c.Namers.KeyType); err != nil {
		return
	}
	if err = initNamers("table", &d.Namers.Table, &c.Namers.Table); err != nil {
		return
	}
	if err = initNamers("schema", &d.Namers.Schema, &c.Namers.Schema); err != nil {
		return
	}
	return
}

type nopNamer struct{}

func (nopNamer) Apply(s string) string { return s }
func (nopNamer) Parse(s string) string { return s }
