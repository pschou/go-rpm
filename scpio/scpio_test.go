package scpio

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"testing"
)

type testcase struct {
	data string
	ino  uint32
}

var cases = []testcase{
	{"foo", 0},
	{"A", 1},
	{"", 2},
	{"bar", 0},
	{"", 3},
	{"baz", 0},
	{"C", 4},
}

func align(b *bytes.Buffer) {
	b.Write(zb[:((b.Len()+0x3)&^0x3)-b.Len()])
}

func makeData() *bytes.Buffer {
	r := new(bytes.Buffer)
	for _, v := range cases {
		fmt.Fprintf(r, "%s%08x\x00\x00%s", scpioMagic, v.ino, v.data)
		align(r)
	}
	r.Write(trailer)
	align(r)
	return r
}

func TestReader(t *testing.T) {
	b := makeData()
	r := NewReader(b)
	var last int
	for i, v := range cases {
		ino, err := r.Next(last)
		if err != nil {
			t.Fatalf("read error, %d: %v", i, err)
		}
		if ino != v.ino {
			t.Fatalf("ino != want, %d: %d != %d", i, ino, v.ino)
		}

		data := b.Next(len(v.data))
		if a, b := string(data), v.data; a != b {
			t.Fatalf("data != want, %d: %q != %q", i, a, b)
		}
		last = len(data)
	}
	if _, err := r.Next(last); err != nil {
		t.Fatalf("read error: %v", err)
	}
}

func comp(t *testing.T, a, b *bytes.Buffer, w *Writer) {
	have := b.Bytes()
	want := a.Next(len(have))
	if !bytes.Equal(have, want) {
		t.Fatalf("want != have, offset: 0x%x\nhave:\n%s\nwant:\n%s",
			w.off, hex.Dump(want), hex.Dump(have))
	}
	b.Reset()
}

func TestWriter(t *testing.T) {
	data := makeData()
	b := new(bytes.Buffer)
	w := NewWriter(b)
	for _, v := range cases {
		w.WriteHeader(v.ino)
		io.WriteString(w, v.data)
		comp(t, data, b, w)
	}
	w.Close()
	comp(t, data, b, w)

	if data.Len() != 0 {
		t.Fatalf("unread bytes, offset: 0x%x\n%s",
			w.off, hex.Dump(data.Bytes()))
	}
}
