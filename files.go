package rpm

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"text/tabwriter"
	"time"
)

const (
	typeDir     = 004
	typeRegular = 010
	typeSymlink = 012
)

type prefixMap struct {
	s []string
	m map[string]int
}

func (p *prefixMap) index(file string) (string, int) {
	d, f := filepath.Split(file)
	if d == "" {
		d = "/"
	}
	if i, ok := p.m[d]; ok {
		return f, i
	}
	i := len(p.m)
	p.m[d] = i
	p.s = append(p.s, d)
	return f, i
}

func newPrefixMap() *prefixMap {
	return &prefixMap{m: make(map[string]int)}
}

type FileIndex struct {
	dirNames   *prefixMap // RPMTAG_DIRNAMES
	dirIndexes []uint32   // RPMTAG_DIRINDEXES
	name       []string   // RPMTAG_BASENAMES
	user       []string   // RPMTAG_FILEUSERNAME
	group      []string   // RPMTAG_FILEGROUPNAME
	dev        []uint32   // RPMTAG_FILEDEVICES
	ino        []uint32   // RPMTAG_FILEINODES
	mtime      []uint32   // RPMTAG_FILEMTIMES
	mode       []uint16   // RPMTAG_FILEMODES
	linkto     []string   // RPMTAG_FILELINKTOS
	digest     []string   // RPMTAG_FILEDIGESTS
	flags      []uint32   // RPMTAG_FILEFLAGS, RPMFILE_CONFIG/DOC/LICENCE/GHOST
	verify     []uint32   // RPMTAG_FILEVERIFYFLAGS, all -1
	size       []uint32   // RPMTAG_FILESIZES
	lsize      []uint64   // RPMTAG_LONGFILESIZES
	rpmsize    uint32     // RPMTAG_SIZE
	rpmlsize   uint64     // RPMTAG_LONGSIZE
}

func NewFileIndex() *FileIndex {
	return &FileIndex{dirNames: newPrefixMap()}
}

type File struct {
	Name     string
	User     string
	Group    string
	Mode     uint16
	LinkTo   string
	MTime    uint32
	Digest   string
	NoVerify uint32
	Size     uint64
	Flags    uint32 // %ghost/config etc
}

var errInvalidFileMode = errors.New("rpm: invalid filemode")

func Mode(mode os.FileMode) (uint16, error) {
	var r uint16
	switch mode & os.ModeType {
	case 0:
		r = typeRegular
	case os.ModeDir:
		r = typeDir
	case os.ModeSymlink:
		r = typeSymlink
	default:
		return 0, errInvalidFileMode
	}
	return r<<12 | uint16(mode&os.ModePerm), nil
}

func (f *FileIndex) Add(r *File) {
	name, di := f.dirNames.index(r.Name)
	f.dirIndexes = append(f.dirIndexes, uint32(di))
	f.name = append(f.name, name)
	f.mode = append(f.mode, r.Mode)
	f.mtime = append(f.mtime, r.MTime)
	f.verify = append(f.verify, ^r.NoVerify)
	f.linkto = append(f.linkto, r.LinkTo)
	f.digest = append(f.digest, r.Digest)
	f.flags = append(f.flags, r.Flags)

	// this can be empty string but rpm throws a warning
	// "user  does not exist - using root"
	f.user = append(f.user, def(r.User, "", "root"))
	f.group = append(f.group, def(r.Group, "", "root"))

	// TODO: fallback to 32b when used
	f.lsize = append(f.lsize, r.Size)
	f.rpmlsize += r.Size
}

func (f *FileIndex) Append(hdr *Header) {
	if len(f.name) == 0 {
		return
	}
	hdr.AddStringArray(RPMTAG_DIRNAMES, f.dirNames.s...)
	hdr.AddStringArray(RPMTAG_BASENAMES, f.name...)
	hdr.AddStringArray(RPMTAG_FILEUSERNAME, f.user...)
	hdr.AddStringArray(RPMTAG_FILEGROUPNAME, f.group...)
	hdr.AddStringArray(RPMTAG_FILELINKTOS, f.linkto...)
	hdr.AddStringArray(RPMTAG_FILEDIGESTS, f.digest...)
	hdr.AddInt32(RPMTAG_DIRINDEXES, f.dirIndexes...)
	hdr.AddInt32(RPMTAG_FILEMTIMES, f.mtime...)
	hdr.AddInt16(RPMTAG_FILEMODES, f.mode...)
	hdr.AddInt32(RPMTAG_FILEFLAGS, f.flags...)
	hdr.AddInt32(RPMTAG_FILEVERIFYFLAGS, f.verify...)
	if f.lsize != nil {
		hdr.AddInt64(RPMTAG_LONGFILESIZES, f.lsize...)
		hdr.AddInt64(RPMTAG_LONGSIZE, f.rpmlsize)
	} else {
		hdr.AddInt32(RPMTAG_FILESIZES, f.size...)
		hdr.AddInt32(RPMTAG_SIZE, f.rpmsize)
	}
}

func FileIndexHeader(hdr *Header) (*FileIndex, error) {
	idx := NewFileIndex()
	var (
		ok  bool
		err error = errTagType
	)

	for _, v := range hdr.Tags {
		switch v.Tag {
		case RPMTAG_DIRNAMES:
			if idx.dirNames.s, ok = v.StringArray(); !ok {
				break
			}
			for i, v := range idx.dirNames.s {
				if _, ok := idx.dirNames.m[v]; ok {
					continue
				}
				idx.dirNames.m[v] = i
			}
		case RPMTAG_BASENAMES:
			idx.name, ok = v.StringArray()
		case RPMTAG_FILEUSERNAME:
			idx.user, ok = v.StringArray()
		case RPMTAG_FILEGROUPNAME:
			idx.group, ok = v.StringArray()
		case RPMTAG_FILELINKTOS:
			idx.linkto, ok = v.StringArray()
		case RPMTAG_FILEDIGESTS:
			idx.digest, ok = v.StringArray()
		case RPMTAG_DIRINDEXES:
			idx.dirIndexes, ok = v.data.(tagUint32)
		case RPMTAG_FILEDEVICES:
			idx.dev, ok = v.data.(tagUint32)
		case RPMTAG_FILEINODES:
			idx.ino, ok = v.data.(tagUint32)
		case RPMTAG_FILEMTIMES:
			idx.mtime, ok = v.data.(tagUint32)
		case RPMTAG_FILEFLAGS:
			idx.flags, ok = v.data.(tagUint32)
		case RPMTAG_FILEVERIFYFLAGS:
			idx.verify, ok = v.data.(tagUint32)
		case RPMTAG_FILEMODES:
			idx.mode, ok = v.data.(tagUint16)
		case RPMTAG_FILESIZES:
			idx.size, ok = v.data.(tagUint32)
		case RPMTAG_LONGFILESIZES:
			idx.lsize, ok = v.data.(tagUint64)
		case RPMTAG_SIZE:
			var sz tagUint32
			if sz, ok = v.data.(tagUint32); ok {
				idx.rpmsize = sz[0]
			}
		case RPMTAG_LONGSIZE:
			var sz tagUint64
			if sz, ok = v.data.(tagUint64); ok {
				idx.rpmlsize = sz[0]
			}
		default:
			continue
		}
		if !ok {
			return nil, err
		}
	}

	return idx, nil
}

func osMode(mode uint16) os.FileMode {
	var r os.FileMode
	switch mode >> 12 {
	case typeDir:
		r |= os.ModeDir
	case typeSymlink:
		r |= os.ModeSymlink
	case typeRegular:
		// no mode for regular files
	}
	return r | os.FileMode(mode)&os.ModePerm
}

func (f *FileIndex) fsize(idx int) uint64 {
	if len(f.lsize) > idx {
		return f.lsize[idx]
	}
	if len(f.size) > idx {
		return uint64(f.size[idx])
	}
	return 0
}

func hexEncode(m uint32) string {
	const encodeHex = "0123456789ABCDEFGHIJKLMNOPQRSTUV"
	if m == 0 {
		return "-"
	}
	if ^m == 0 {
		return "!"
	}
	var r [32]byte
	var b int
	for i, v := range encodeHex {
		if m>>i&0x1 == 0 {
			continue
		}
		r[b] = byte(v)
		b++
	}
	return string(r[:b])
}

func def(a, b, d string) string {
	if a == b {
		return d
	}
	return a
}

func (f *FileIndex) file(i int) string {
	d, n, l := f.dirIndexes[i], f.name[i], f.linkto[i]
	if l != "" {
		l = " -> " + l
	}
	return path.Join(f.dirNames.s[d], n) + l
}

func (f *FileIndex) dumpIndex(w io.Writer, i int) error {
	_, err := fmt.Fprintln(w,
		hexEncode(^f.verify[i]),
		"\t", hexEncode(f.flags[i]),
		"\t", def(f.digest[i], "", "-"),
		"\t", osMode(f.mode[i]),
		"\t", def(f.user[i], "root", "-"),
		"\t", def(f.group[i], "root", "-"),
		"\t", f.fsize(i),
		"\t", time.Unix(int64(f.mtime[i]), 0).UTC().
			Format(time.RFC3339),
		"\t", f.file(i),
	)
	return err
}

func (f *FileIndex) Dump(w io.Writer) error {
	if len(f.name) == 0 {
		return nil
	}

	for i, v := range []int{
		len(f.verify),
		len(f.flags),
		len(f.digest),
		len(f.user),
		len(f.group),
		len(f.mtime),
		len(f.dirIndexes),
		len(f.linkto),
	} {
		if v != len(f.name) {
			return fmt.Errorf("rpm: invalid file index: %d", i)
		}
	}

	tw := tabwriter.NewWriter(w, 0, 2, 0, ' ', 0)
	for i := range f.name {
		if err := f.dumpIndex(tw, i); err != nil {
			return err
		}
	}
	return tw.Flush()
}
