package rpm

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"sort"
	"strconv"
	"testing"
)

func makeHeader(t *testing.T, w io.Writer, xpre *rpmHeaderPre, tags ...*Tag) int {
	var l, n int
	for _, v := range tags {
		if v.Type == 0 {
			l += int(v.Count)
			continue
		}
		l += v.data.Len()
		n++
	}

	pre := &rpmHeaderPre{
		Magic:  rpmHeaderMagic,
		Count:  uint32(n),
		Length: uint32(l),
	}
	if xpre != nil {
		pre = xpre
	}

	if err := binary.Write(w, binary.BigEndian, pre); err != nil {
		t.Fatalf("hdr write: %v", err)
	}

	for i, v := range tags {
		if v.Type == 0 {
			continue
		}
		if err := v.writeHeader(w); err != nil {
			t.Fatalf("tag(%d) writeheader: %v", i, err)
		}
	}

	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Tag < tags[j].Tag
	})

	for i, v := range tags {
		if v.Type == 0 {
			w.Write(bytes.Repeat([]byte{0x99}, int(v.Count)))
			continue
		}
		if _, err := v.data.WriteTo(w); err != nil {
			t.Fatalf("tag(%d) writedata: %v", i, err)
		}
	}

	return n
}

func makeTag(tag, t, count, offset uint32, data tagData) *Tag {
	return &Tag{
		tagHeader: tagHeader{
			Tag:    TagType(tag),
			Type:   t,
			Count:  count,
			Offset: offset,
		},
		data: data,
	}
}

func pad(i, n uint32) *Tag { return makeTag(i, 0, n, 0, nil) }

func tagEq(t *testing.T, t1, t2 *Tag) {
	if a, b := t1.tagHeader, t2.tagHeader; a != b {
		t.Fatalf("tag hdr: \nwant %#v\nhave %#v", a, b)
	}

	var b1, b2 bytes.Buffer
	if _, err := t1.data.WriteTo(&b1); err != nil {
		t.Fatalf("tag1 data: %v", err)
	}
	if _, err := t2.data.WriteTo(&b2); err != nil {
		t.Fatalf("tag2 data: %v", err)
	}

	if a, b := b1.Bytes(), b2.Bytes(); !bytes.Equal(a, b) {
		t.Fatalf("t1.data != t2.data\n%s\n%s",
			hex.Dump(a), hex.Dump(b))
	}
}

func validate(t *testing.T, xerr error, xhdr *rpmHeaderPre, tags ...*Tag) {
	b := new(bytes.Buffer)
	pn := makeHeader(t, b, xhdr, tags...)

	r := NewReader(b)
	hdr, err := r.Next()
	if !errors.Is(err, xerr) {
		t.Fatalf("expected error: %v\ngot: %v", xerr, err)
	}
	if err != nil {
		return
	}

	if a, b := len(hdr.Tags), pn; a != b {
		t.Fatalf("hdr length: hdr:%d, want:%d", a, b)
	}

	var n int
	for i, v := range tags {
		if v.Type == 0 {
			n++
			continue
		}
		tagEq(t, v, hdr.Tags[i-n])
	}
}

func TestReader(t *testing.T) {
	t.Run("ordered", func(t *testing.T) {
		validate(t, nil, nil,
			makeTag(0, RPM_STRING_TYPE, 1, 0, &tagString{
				data: []string{"foobar"},
			}),
			pad(1, 1),
			makeTag(2, RPM_INT16_TYPE, 1, 8, tagUint16{0xdead}),
			pad(3, 2),
			makeTag(4, RPM_INT32_TYPE, 1, 12, tagUint32{0xdeadbeef}),
			makeTag(5, RPM_INT64_TYPE, 1, 16, tagUint64{0x1122334455667788}),
			makeTag(6, RPM_BIN_TYPE, 9, 24, &tagBytes{
				b: bytes.NewBufferString("foobarbaz")},
			),
		)
	})

	t.Run("unordered", func(t *testing.T) {
		validate(t, nil, nil,
			makeTag(2, RPM_INT32_TYPE, 1, 4, tagUint32{0xdeadbeef}),
			makeTag(3, RPM_INT64_TYPE, 1, 8, tagUint64{0x1122334455667788}),
			makeTag(1, RPM_INT16_TYPE, 2, 0, tagUint16{0xdead, 0xbeef}),
		)
	})

	t.Run("oob/offset", func(t *testing.T) {
		validate(t, errOffsetOOB, nil,
			makeTag(0, RPM_INT32_TYPE, 1, 0, tagUint32{0xdead}),
			// should start at 4
			makeTag(2, RPM_INT32_TYPE, 1, 8, tagUint32{0xbeef}),
		)
	})

	t.Run("oob/hdr", func(t *testing.T) {
		validate(t, errOffsetOOB, nil,
			// offset outside of length
			makeTag(0, RPM_INT32_TYPE, 1, 8, tagUint32{0xdead}),
		)
	})

	t.Run("header/magic", func(t *testing.T) {
		b := new(bytes.Buffer)
		if err := binary.Write(b, binary.BigEndian,
			&rpmHeaderPre{Magic: [8]byte{0xde, 0xad, 0xbe, 0xef}},
		); err != nil {
			t.Fatalf("hdr write: %v", err)
		}
		_, err := NewReader(b).Next()
		if !errors.Is(err, errInvalidHeader) {
			t.Fatalf("expected header error, got: %v", err)
		}
	})

	for _, v := range []int{1, 10} {
		t.Run("header/length+"+strconv.Itoa(v), func(t *testing.T) {
			validate(t,
				errUnexpectedEOF,
				&rpmHeaderPre{
					Magic:  rpmHeaderMagic,
					Count:  3,
					Length: 4*2 + 8 + 2*2 + uint32(v),
				},
				makeTag(1, RPM_INT32_TYPE, 2, 0, tagUint32{0xdead, 0xbeef}),
				makeTag(2, RPM_INT64_TYPE, 1, 8, tagUint64{0x1122334455667788}),
				makeTag(3, RPM_INT16_TYPE, 2, 16, tagUint16{0xdead, 0xbeef}),
			)
		})
	}
}
