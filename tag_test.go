package rpm

import (
	"bytes"
	"encoding/json"
	"testing"
)

var tagTypes = []uint32{
	RPM_BIN_TYPE,
	RPM_CHAR_TYPE,
	RPM_I18NSTRING_TYPE,
	RPM_INT16_TYPE,
	RPM_INT32_TYPE,
	RPM_INT64_TYPE,
	RPM_INT8_TYPE,
	RPM_STRING_ARRAY_TYPE,
	RPM_STRING_TYPE,
}

func makeTagData(t uint32) (tagData, uint32) {
	switch t {
	case
		RPM_STRING_TYPE,
		RPM_I18NSTRING_TYPE:
		return &tagString{
			data: []string{"foo"},
			len:  3 + 1,
		}, 1
	case RPM_STRING_ARRAY_TYPE:
		return &tagString{
			data: []string{"foo", "bar"},
			len:  3*2 + 2,
		}, 2
	case RPM_INT16_TYPE:
		return tagUint16{0xdead, 0xbeef}, 2
	case RPM_INT32_TYPE:
		return tagUint32{0xdeadbeef, 0x11223344}, 2
	case RPM_INT64_TYPE:
		return tagUint64{0x1122334455667788, 0xdeadbeef11112222}, 2
	default:
		b := bytes.NewBufferString("foobar")
		return &tagBytes{b: b}, uint32(b.Len())
	}
}

func TestTagJSON(t *testing.T) {
	for i, v := range tagTypes {
		tag := new(Tag)
		tag.Type = v
		tag.data, tag.Count = makeTagData(v)

		b, err := json.Marshal(tag)
		if err != nil {
			t.Errorf("marshal error, idx %d, v:%d, %v", i, v, err)
		}

		jt := new(Tag)
		if err := json.Unmarshal(b, jt); err != nil {
			t.Errorf("unmarshal error, idx %d, v:%d, %v", i, v, err)
		}

		tagEq(t, tag, jt)
	}
}
