package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"strings"

	"github.com/tlahdekorpi/rpm"
	"github.com/tlahdekorpi/rpm/scpio"
)

func index(r io.Reader, w *scpio.Writer) (*rpm.FileIndex, error) {
	var (
		idx = rpm.NewFileIndex()
		tr  = tar.NewReader(r)
		i   uint32
	)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		mode, err := rpm.Mode(hdr.FileInfo().Mode())
		if err != nil {
			return nil, err
		}
		file := &rpm.File{
			Name:   path.Join("/", hdr.Name),
			LinkTo: hdr.Linkname,
			MTime:  uint32(hdr.ModTime.Unix()),
			Size:   uint64(hdr.Size),
			Mode:   mode,
		}

		if err := w.WriteHeader(i); err != nil {
			return nil, err
		}
		i++

		if hdr.Typeflag != tar.TypeReg {
			idx.Add(file)
			continue
		}

		sum := sha256.New()
		n, err := io.Copy(io.MultiWriter(w, sum), tr)
		if err != nil {
			return nil, err
		}

		if n != hdr.Size {
			return nil, fmt.Errorf(
				"hdr size mismatch, want %d, have %d",
				n, hdr.Size,
			)
		}

		file.Digest = hex.EncodeToString(sum.Sum(nil))
		idx.Add(file)
	}
	return idx, w.Close()
}

type Config struct {
	Name        string
	Version     string
	Release     string
	Arch        string
	License     string
	URL         string
	BugURL      string `name:"bug-url"`
	Packager    string
	Vendor      string
	Summary     string
	Description string
	Provides    []string
	Requires    []string
	PreInstall  script
	PostInstall script
}

type sense struct {
	name    string
	version string
	flags   uint32
}

func senseFlags(value string) sense {
	i := strings.IndexAny(value, "<>=")
	if i == -1 {
		return sense{name: value, flags: rpm.RPMSENSE_ANY}
	}
	r := sense{name: value[:i]}
	for j, v := range value[i:] {
		switch v {
		case '<':
			r.flags |= rpm.RPMSENSE_LESS
		case '>':
			r.flags |= rpm.RPMSENSE_GREATER
		case '=':
			r.flags |= rpm.RPMSENSE_EQUAL
		default:
			r.version = value[i+j:]
			return r
		}
	}
	return r
}

func (c *Config) provides(hdr *rpm.Header) {
	c.Provides = append(c.Provides,
		c.Name+"="+c.Version+"-"+c.Release,
	)
	var (
		flags   []uint32
		names   []string
		version []string
	)
	pm := make(map[string]struct{})
	for _, p := range c.Provides {
		if _, ok := pm[p]; ok {
			continue
		}
		s := senseFlags(p)
		pm[s.name] = struct{}{}
		flags = append(flags, s.flags)
		names = append(names, s.name)
		version = append(version, s.version)
	}
	hdr.AddInt32(rpm.RPMTAG_PROVIDEFLAGS, flags...)
	hdr.AddStringArray(rpm.RPMTAG_PROVIDENAME, names...)
	hdr.AddStringArray(rpm.RPMTAG_PROVIDEVERSION, version...)
}

func (c *Config) requires(hdr *rpm.Header) {
	if len(c.Requires) == 0 {
		return
	}
	var (
		flags   []uint32
		names   []string
		version []string
	)
	rm := make(map[string]struct{})
	for _, p := range c.Requires {
		if _, ok := rm[p]; ok {
			continue
		}
		s := senseFlags(p)
		rm[s.name] = struct{}{}
		flags = append(flags, s.flags)
		names = append(names, s.name)
		version = append(version, s.version)
	}
	hdr.AddInt32(rpm.RPMTAG_REQUIREFLAGS, flags...)
	hdr.AddStringArray(rpm.RPMTAG_REQUIRENAME, names...)
	hdr.AddStringArray(rpm.RPMTAG_REQUIREVERSION, version...)
}

func add(hdr *rpm.Header, t rpm.TagType, v string) {
	if v == "" {
		return
	}
	hdr.AddString(t, v)
}

func (c *Config) append(hdr *rpm.Header) {
	add(hdr, rpm.RPMTAG_NAME, c.Name)
	add(hdr, rpm.RPMTAG_VERSION, c.Version)
	add(hdr, rpm.RPMTAG_RELEASE, c.Release)
	add(hdr, rpm.RPMTAG_ARCH, c.Arch)
	add(hdr, rpm.RPMTAG_LICENSE, c.License)
	add(hdr, rpm.RPMTAG_URL, c.URL)
	add(hdr, rpm.RPMTAG_BUGURL, c.BugURL)
	add(hdr, rpm.RPMTAG_PACKAGER, c.Packager)
	add(hdr, rpm.RPMTAG_VENDOR, c.Vendor)
	add(hdr, rpm.RPMTAG_SUMMARY, c.Summary)
	add(hdr, rpm.RPMTAG_DESCRIPTION, c.Description)

	if c.PreInstall.data != "" {
		hdr.AddString(rpm.RPMTAG_PREIN, c.PreInstall.data)
		hdr.AddString(rpm.RPMTAG_PREINPROG, c.PreInstall.prog)
	}
	if c.PostInstall.data != "" {
		hdr.AddString(rpm.RPMTAG_POSTIN, c.PostInstall.data)
		hdr.AddString(rpm.RPMTAG_POSTINPROG, c.PostInstall.prog)
	}

	c.provides(hdr)
	c.requires(hdr)
}

var flagConfig = flag.String("c", "", "config file")

func main() {
	log.SetFlags(0)
	log.SetPrefix("tar2rpm: ")
	flag.Parse()

	config := &Config{
		Name:    "package",
		Version: "1",
		Release: "1",
		Arch:    "noarch",
	}

	if *flagConfig != "" {
		f, err := os.Open(*flagConfig)
		if err != nil {
			log.Fatal(err)
		}
		if err := loadconfig(f, config); err != nil {
			log.Fatal(err)
		}
		f.Close()
	}

	hdr := rpm.NewPayloadHeader()
	config.append(hdr)

	// TODO: write payload to disk
	data := new(bytes.Buffer)
	sum := sha256.New()
	idx, err := index(os.Stdin, scpio.NewWriter(
		io.MultiWriter(data, sum),
	))
	if err != nil {
		log.Fatal(err)
	}

	hdr.AddStringArray(rpm.RPMTAG_HEADERI18NTABLE, "C")
	hdr.AddString(rpm.RPMTAG_ENCODING, "utf-8")
	hdr.AddString(rpm.RPMTAG_PAYLOADFORMAT, "cpio")
	hdr.AddString(rpm.RPMTAG_OS, "linux")
	hdr.AddInt32(rpm.RPMTAG_BUILDTIME, 0) // rpm requires

	hdr.AddInt32(rpm.RPMTAG_PAYLOADDIGESTALGO, rpm.PGPHASHALGO_SHA256)
	hdr.AddInt32(rpm.RPMTAG_FILEDIGESTALGO, rpm.PGPHASHALGO_SHA256)
	hdr.AddStringArray(rpm.RPMTAG_PAYLOADDIGEST, hex.EncodeToString(sum.Sum(nil)))

	idx.Append(hdr)

	pb := new(bytes.Buffer)
	hs := sha256.New()
	if _, err := hdr.WriteTo(io.MultiWriter(pb, hs)); err != nil {
		log.Fatal(err)
	}

	sig := rpm.NewSignatureHeader()
	sig.AddString(rpm.RPMSIGTAG_SHA256, hex.EncodeToString(hs.Sum(nil)))

	buf := bufio.NewWriterSize(os.Stdout, 1<<20)
	if _, err := rpm.WriteHeaders(buf,
		rpm.NewLead(strings.Join(
			[]string{config.Name, config.Version, config.Release},
			"-",
		), rpm.LeadBinary),
		sig,
		pb,
	); err != nil {
		log.Fatal(err)
	}

	if _, err := io.Copy(buf, data); err != nil {
		log.Fatal(err)
	}
	if err := buf.Flush(); err != nil {
		log.Fatal(err)
	}
}
