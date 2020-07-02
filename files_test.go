package rpm

import (
	"bytes"
	"testing"
)

func TestPrefixMap(t *testing.T) {
	// TODO: /file1/file2 failure
	pm := newPrefixMap()
	for _, v := range []struct {
		add  string
		idx  int
		name string
	}{
		{"/file1", 0, "file1"},
		{"/dir1/file2", 1, "file2"},
		{"/file3", 0, "file3"},
		{"/dir2/file4", 2, "file4"},
		{"nosep", 0, "nosep"},
	} {
		n, i := pm.index(v.add)
		if v.idx != i {
			t.Errorf("index: %d != %d", v.idx, i)
		}
		if v.name != n {
			t.Errorf("name: %s != %s", v.name, n)
		}
	}
}

func diff(t *testing.T, a, b *FileIndex) {
	var b1, b2 bytes.Buffer
	if err := a.Dump(&b1); err != nil {
		t.Fatal(err)
	}
	if err := b.Dump(&b2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1.Bytes(), b2.Bytes()) {
		t.Fatalf("a != b\n%s\n%s", &b1, &b2)
	}
}

func TestFileIndex(t *testing.T) {
	fi := NewFileIndex()
	for i, v := range []*File{
		{Name: "/dir/file1", User: "foo"},
		{Name: "/dir/file2", Group: "bar"},
		{Name: "/dir"},
		{Name: "/foo", LinkTo: "bar"},
	} {
		v.Size = uint64(i)
		fi.Add(v)
	}
	hdr := new(Header)
	fi.Append(hdr)

	idx, err := FileIndexHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}

	diff(t, idx, fi)
}
