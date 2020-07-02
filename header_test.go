package rpm

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"testing"
)

func makeHdr() *Header {
	hdr := new(Header)
	hdr.AddString(1, "foo")
	hdr.AddStringI18N(1, "I18N")
	hdr.AddStringArray(2, "foo", "bar", "baz")
	hdr.AddInt16(3, 0x1122, 0x3344, 0x5566)
	hdr.AddInt32(4, 0x11223344, 0x55667788, 0x99112233)
	hdr.AddInt64(5, 0x1122334455667788, 0x99, 0xff)
	hdr.AddBin(6, []byte("foo"))
	return hdr
}

func TestHeaderWrite(t *testing.T) {
	hdr := makeHdr()

	b := new(bytes.Buffer)
	n, err := hdr.WriteTo(b)
	if err != nil {
		t.Fatalf("hdr write: %v", err)
	}

	if a, b := n, int64(b.Len()); a != b {
		t.Fatalf("hdr WriteTo length: want %d, have %d", a, b)
	}

	have, err := NewReader(b).Next()
	if err != nil {
		t.Fatalf("hdr read: %v", err)
	}

	if a, b := hdr.Len(), have.Len(); a != b {
		t.Fatalf("hdr length: want %d, have %d", a, b)
	}

	for i := 0; i < hdr.Len(); i++ {
		tagEq(t, hdr.Tags[i], have.Tags[i])
	}
}

func TestHeaderRegion(t *testing.T) {
	const tt = 0xdeadbeef
	hdr := makeHdr()
	hdr.SetRegion(tt)

	b := new(bytes.Buffer)
	if _, err := hdr.WriteTo(b); err != nil {
		t.Fatalf("hdr write: %v", err)
	}

	have, err := NewReader(b).Next()
	if err != nil {
		t.Fatalf("hdr read: %v", err)
	}

	lt := have.Tags[len(have.Tags)-1]
	tagEq(t, lt, hdr.region)

	want := tagHeader{
		Tag:    tt,
		Type:   RPM_BIN_TYPE,
		Offset: uint32(-int32(len(hdr.Tags)+1) * tagSize),
		Count:  tagSize,
	}

	var th tagHeader
	if err := binary.Read(
		hdr.region.data.(*tagBytes).b,
		binary.BigEndian,
		&th,
	); err != nil {
		t.Fatalf("region data read: %v", err)
	}

	if th != want {
		t.Fatalf("invalid region data\nwant: %#v\nhave: %#v", want, th)
	}
}

func hdrEq(t *testing.T, hdr, have *Header) {
	if a, b := hdr.Len(), have.Len(); a != b {
		t.Fatalf("hdr length: want %d, have %d", a, b)
	}

	for i := 0; i < hdr.Len(); i++ {
		tagEq(t, hdr.Tags[i], have.Tags[i])
	}

	if hdr.region != nil {
		if err := have.setRegion(new(rpmHeaderPre)); err != nil {
			t.Fatalf("hdr setRegion: %v", err)
		}
		tagEq(t, hdr.region, have.region)
	}

	var b1, b2 bytes.Buffer
	if _, err := hdr.WriteTo(&b1); err != nil {
		t.Fatalf("hdr writeTo: %v", err)
	}
	if _, err := have.WriteTo(&b2); err != nil {
		t.Fatalf("have writeTo: %v", err)
	}
	if a, b := b1.Len(), b2.Len(); a != b {
		t.Fatalf("hdr bytes length: want %d, have %d", a, b)
	}
	if a, b := b1.Bytes(), b2.Bytes(); !bytes.Equal(a, b) {
		t.Fatalf("hdr bytes: want != have\n%s\n%s",
			hex.Dump(a), hex.Dump(b))
	}
}

func testHeaderJSON(t *testing.T, hdr *Header) {
	jb, err := json.Marshal(hdr)
	if err != nil {
		t.Fatalf("hdr json marshal: %v", err)
	}

	have := new(Header)
	if err := json.Unmarshal(jb, have); err != nil {
		t.Fatalf("hdr json unmarshal: %v", err)
	}

	hdrEq(t, hdr, have)
}

func TestHeaderJSON(t *testing.T) {
	t.Run("no-region", func(t *testing.T) {
		testHeaderJSON(t, makeHdr())
	})
	t.Run("region", func(t *testing.T) {
		hdr := makeHdr()
		hdr.SetRegion(HEADER_IMMUTABLE)
		testHeaderJSON(t, hdr)
	})
}

func TestWriteHeaders(t *testing.T) {
	lead := NewLead("test", LeadBinary)
	h1, h2 := makeHdr(), makeHdr()
	h1.SetRegion(HEADER_SIGNATURES)
	h2.SetRegion(HEADER_IMMUTABLE)

	b := new(bytes.Buffer)
	n, err := WriteHeaders(b, lead, h1, h2)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if a, b := n, int64(b.Len()); a != b {
		t.Fatalf("length: want %d, have %d", a, b)
	}

	r := NewReader(b)

	t.Run("lead", func(t *testing.T) {
		l, err := r.Lead()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if *l != *lead {
			t.Fatalf("invalid lead:\nhave: %x\nwant: %x", l, lead)
		}
	})

	for i, v := range []*Header{h1, h2} {
		t.Run("hdr"+strconv.Itoa(i+1), func(t *testing.T) {
			h, err := r.Next()
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			hdrEq(t, v, h)
		})
	}
}
