package sqlmodelgen

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/skillian/expr/errors"
	"github.com/skillian/expr/stream/sqlstream/config"
	"github.com/skillian/expr/stream/sqlstream/sqltypes"
)

var (
	JSONModelContext interface {
		ModelContext
		ModelConfigWriter
	} = jsonModelContext{}
)

type jsonModelContext struct{}

func (jsonModelContext) ModelType(t sqltypes.Type) (namespace, typename string, err error) {
	return t.String(), "", nil
}

func (jsonModelContext) WriteModelConfig(w io.Writer, c config.Config) error {
	bs, err := json.Marshal(c)
	if err != nil {
		return errors.Errorf0From(
			err, "error serializing configuration into JSON",
		)
	}
	if _, err = io.Copy(w, bytes.NewReader(bs)); err != nil {
		return errors.Errorf1From(
			err, "failed to write JSON to %v", w,
		)
	}
	return nil
}
