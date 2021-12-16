package sqlmodelgen

import (
	"context"
	"encoding/xml"
	"io"
	"io/ioutil"
	"strings"

	"github.com/skillian/expr/errors"
	"github.com/skillian/expr/stream/sqlstream/config"
	"github.com/skillian/expr/stream/sqlstream/sqltypes"
	"github.com/skillian/logging"
)

var (
	DrawIOModelContext interface {
		ModelContext
		ModelConfigParser
	} = drawIOModelContext{}

	logger = logging.GetLogger("sqlmodelgen")
)

type drawIOModelContext struct{}

func (drawIOModelContext) ModelType(t sqltypes.Type) (namespace, typename string, err error) {
	return "", t.String(), nil
}

type DrawIOXMLObject struct {
	ID string `xml:"id,attr"`
}

type DrawIOXMLMXFile struct {
	XMLName xml.Name         `xml:"mxfile"`
	Version string           `xml:"version,attr"`
	Diagram DrawIOXMLDiagram `xml:"diagram"`
}

type DrawIOXMLDiagram struct {
	DrawIOXMLObject
	Name       string                `xml:"name,attr"`
	GraphModel DrawIOXMLMXGraphModel `xml:"mxGraphModel"`
}

type DrawIOXMLMXGraphModel struct {
	Root DrawIOXMLRoot `xml:"root"`
}

type DrawIOXMLRoot struct {
	Cells []DrawIOXMLCell `xml:"mxCell"`
}

type DrawIOXMLCell struct {
	DrawIOXMLObject
	Parent string `xml:"parent,attr"`
	Value  string `xml:"value,attr"`
	Source string `xml:"source,attr"`
	Target string `xml:"target,attr"`
	Style  string `xml:"style,attr"`
}

type DrawIOCell struct {
	Kind     DrawIOCellKind
	ID       string
	Name     string
	Type     string
	Parent   *DrawIOCell
	Children []*DrawIOCell
	Source   *DrawIOCell
	Target   *DrawIOCell
	Style    DrawIOCellStyle
	// PK is true when the cell value ends with ": pk"
	PK bool
}

type DrawIOCellKind int

func drawIOCellKindOf(c *DrawIOCell) (k DrawIOCellKind) {
	switch {
	case c.Style.Kind == DrawIOCellStyleText &&
		c.Parent != nil &&
		c.Parent.Style.Kind == DrawIOCellStyleSwimLane &&
		c.Name != "" && c.Type != "":
		k = DrawIOColumn
	case c.Style.Kind == DrawIOCellStyleSwimLane &&
		len(c.Children) > 0 &&
		c.Children[len(c.Children)-1].Style.Kind == DrawIOCellStyleText:
		k = DrawIOTable
	case c.Source != nil && c.Target != nil:
		k = DrawIOArrow
	default:
		k = DrawIOBadKind
	}
	logger.Verbose2("cell %#v kind: %v", c, k)
	return
}

const (
	DrawIOBadKind DrawIOCellKind = iota
	DrawIOTable
	DrawIOColumn
	DrawIOArrow
)

type DrawIOCellStyleKind int

const (
	DrawIOCellStyleBadKind DrawIOCellStyleKind = iota
	DrawIOCellStyleText
	DrawIOCellStyleSwimLane
)

type DrawIOCellStyle struct {
	Kind       DrawIOCellStyleKind
	StartArrow DrawIOArrowERType
	EndArrow   DrawIOArrowERType
}

func (s *DrawIOCellStyle) setStartArrow(v string) (ok bool) {
	s.StartArrow, ok = drawIOArrowERTypes[v]
	return
}

func (s *DrawIOCellStyle) setEndArrow(v string) (ok bool) {
	s.EndArrow, ok = drawIOArrowERTypes[v]
	return
}

var drawIOCellStyleSetters = map[string]func(*DrawIOCellStyle, string) bool{
	"startArrow": (*DrawIOCellStyle).setStartArrow,
	"endArrow":   (*DrawIOCellStyle).setEndArrow,
}

func drawIOParseCellStyle(v string) (s DrawIOCellStyle, err error) {
	styleString := v
	const (
		parseName = iota
		parseValue
	)
	state := parseName
	name := ""
	next, semi, equals := 0, 0, 0
	for next < len(v) {
		v = v[next:]
		switch state {
		case parseName:
			equals = strings.IndexByte(v, '=')
			if equals == -1 {
				equals = len(v)
			}
			semi = strings.IndexByte(v, ';')
			if semi == -1 {
				semi = len(v)
			}
			if semi < equals || semi == len(v) {
				// style key with no value
				next = semi
				switch v[:next] {
				case "text":
					s.Kind = DrawIOCellStyleText
				case "swimlane":
					s.Kind = DrawIOCellStyleSwimLane
				default:
					logger.Warn1(
						"unhandled name-only style: %v",
						v[:semi],
					)
				}
				if semi < len(v) {
					next++ // skip ';'
				}
				// state = parseValue // still parsing a name
				continue
			}
			// style key with value
			next = equals
			if next == -1 {
				return s, errors.Errorf1(
					"failed to get name from %q", v,
				)
			}
			name = v[:next]
			next++ // skip '='
			state = parseValue
		case parseValue:
			next = strings.IndexByte(v, ';')
			if next == -1 {
				return s, errors.Errorf1(
					"failed to get value from %q", v,
				)
			}
			if fn, ok := drawIOCellStyleSetters[name]; ok {
				// we don't care about all the styles.
				if ok = fn(&s, v[:next]); !ok {
					// but if we have the style, we want
					// to be sure of it.
					return s, errors.Errorf2(
						"invalid %v value: %v",
						name, v[:next],
					)
				}
			}
			next++ // skip ';'
			state = parseName
		}
	}
	logger.Verbose2("parsed %q to style: %#v", styleString, s)
	return
}

type DrawIOArrowERType int

const (
	DrawIOBadArrowERType DrawIOArrowERType = iota
	//DrawIOZeroToOne
	DrawIOZeroToMany
	DrawIOOneToMany
	DrawIOMandOne
)

var drawIOArrowERTypes = map[string]DrawIOArrowERType{
	"ERzeroToMany": DrawIOZeroToMany,
	"ERoneToMany":  DrawIOOneToMany,
	"ERmandOne":    DrawIOMandOne,
}

func DrawIOCellFromXML(r io.Reader) (*DrawIOCell, error) {
	bs, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, errors.Errorf1From(
			err, "failed to read all bytes from %v", r,
		)
	}
	var f DrawIOXMLMXFile
	if err = xml.Unmarshal(bs, &f); err != nil {
		return nil, errors.Errorf2From(
			err, "failed to unmarshal %v bytes of XML into %T",
			len(bs), f,
		)
	}
	root := &DrawIOCell{
		// The diagram (sheet) name gets dropped into the root cell
		// and becomes the database name.
		Name: f.Diagram.Name,
	}
	cells := make(map[string]*DrawIOCell, len(f.Diagram.GraphModel.Root.Cells)+1)
	cells[""] = root
	for _, x := range f.Diagram.GraphModel.Root.Cells {
		cell := &DrawIOCell{
			ID: x.ID,
		}
		if _, ok := cells[x.ID]; ok {
			// sanity check
			return nil, errors.Errorf1(
				"redefinition of cell with ID: %v", x.ID,
			)
		}
		cells[x.ID] = cell
		parent, ok := cells[x.Parent]
		if !ok {
			return nil, errors.Errorf2(
				"failed to find parent with ID %v of cell "+
					"with ID %v",
				x.Parent, x.ID,
			)
		}
		cell.Parent = parent
		parent.Children = append(parent.Children, cell)
		switch {
		case x.Source != "" || x.Target != "":
			// an arrow
			var attrName = ""
			switch {
			case x.Source == "":
				attrName = "source"
			case x.Target == "":
				attrName = "target"
			}
			if attrName != "" {
				return nil, errors.Errorf2(
					"arrow with ID %v has no %v.  "+
						"Please check the diagram to "+
						"make sure it's linked "+
						"properly.",
					x.ID, attrName,
				)
			}
			cell.Source, ok = cells[x.Source]
			if !ok {
				// TODO:  Maybe change cells element type
				// to double ptr so we can define placeholders
				// before the cells are actually defined.
				return nil, errors.Errorf2(
					"failed to get arrow %v source %v",
					x.ID, x.Source,
				)
			}
			cell.Target, ok = cells[x.Target]
			if !ok {
				return nil, errors.Errorf2(
					"failed to get arrow %v target %v",
					x.ID, x.Target,
				)
			}
		case x.Value != "":
			// a cell
			// TODO: Have to distinguish between table and column cells:
			switch parent.Style.Kind {
			case DrawIOCellStyleSwimLane:
				pivot := strings.IndexByte(x.Value, ':')
				if pivot == -1 {
					return nil, errors.Errorf1(
						"failed to get name of cell with ID %v",
						x.ID,
					)
				}
				cell.Name, x.Value = x.Value[:pivot], x.Value[pivot+1:]
				if strings.HasSuffix(x.Value, ": pk") {
					cell.PK, x.Value = true, x.Value[:len(x.Value)-len(": pk")]
				}
				cell.Type = strings.TrimSpace(x.Value)
			default:
				cell.Name = x.Value
			}

		default:
			logger.Warn("ignoring cell with ID %v", x.ID)
		}
		cell.Style, err = drawIOParseCellStyle(x.Style)
		if err != nil {
			return nil, err
		}
	}
	for _, x := range f.Diagram.GraphModel.Root.Cells {
		cell := cells[x.ID]
		cell.Kind = drawIOCellKindOf(cell)
	}
	return root, nil
}

func (drawIOModelContext) ParseModelConfig(ctx context.Context, r io.Reader) (cfg config.Config, err error) {
	root, err := DrawIOCellFromXML(r)
	if err != nil {
		return cfg, err
	}
	cellList := make([]*DrawIOCell, 0, 16)
	var recurseAppendCells func(*DrawIOCell)
	recurseAppendCells = func(c *DrawIOCell) {
		cellList = append(cellList, c)
		for _, child := range c.Children {
			recurseAppendCells(child)
		}
	}
	recurseAppendCells(root)
	cfg.Databases = []config.Database{
		{
			CommonData: config.CommonData{
				Names: config.Names{
					RawName: root.Name,
				},
			},
			Schemas: []config.Schema{
				{
					Tables: make([]config.Table, 0, 8),
				},
			},
		},
	}
	sch := &cfg.Databases[0].Schemas[0]
	tableIdxByID := make(map[string]int)
	tblColIdxByID := make(map[string][2]int)
	for _, cell := range cellList {
		switch cell.Kind {
		case DrawIOTable:
			tableIdxByID[cell.ID] = len(sch.Tables)
			sch.Tables = append(sch.Tables, config.Table{
				CommonData: config.CommonData{
					Names: config.Names{
						RawName: cell.Name,
					},
				},
				Columns: make([]config.Column, 0, len(cell.Children)),
			})
		case DrawIOColumn:
			ti, ok := tableIdxByID[cell.Parent.ID]
			if !ok {
				return cfg, errors.Errorf2(
					"failed to find table (ID: %v) for "+
						"cell (ID: %v)",
					cell.Parent.ID, cell.ID,
				)
			}
			table := &sch.Tables[ti]
			tblColIdxByID[cell.ID] = [2]int{ti, len(table.Columns)}
			table.Columns = append(table.Columns, config.Column{
				CommonData: config.CommonData{
					Names: config.Names{
						RawName: cell.Name,
					},
				},
				Type: cell.Type,
				PK:   cell.PK,
			})
		case DrawIOArrow:
			si, ok := tblColIdxByID[cell.Source.ID]
			if !ok {
				return cfg, errors.Errorf2(
					"failed to find source column (ID: %v) for "+
						"arrow (ID: %v)",
					cell.Source.ID, cell.ID,
				)
			}
			ti, ok := tblColIdxByID[cell.Target.ID]
			if !ok {
				return cfg, errors.Errorf2(
					"failed to find target column (ID: %v) for "+
						"arrow (ID: %v)",
					cell.Target.ID, cell.ID,
				)
			}
			src := &sch.Tables[si[0]].Columns[si[1]]
			trgTbl := &sch.Tables[ti[0]]
			trgCol := &trgTbl.Columns[ti[1]]
			src.FK = strings.Join([]string{
				cfg.Databases[0].RawName,
				sch.RawName,
				trgTbl.RawName,
				trgCol.RawName,
			}, ".")
			src.PK = true // TODO: Should we do this implicitly?
		}
	}
	return
}
