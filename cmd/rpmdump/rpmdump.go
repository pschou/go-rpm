package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/tlahdekorpi/rpm"
)

func dump(w io.Writer, fl bool, h ...*rpm.Header) error {
	for i, v := range h {
		var tt rpm.TagType
		rtag, err := v.Region()
		if err != nil {
			return err
		}

		fmt.Printf("hdr(%d), len:%#x, count:%d\n", i, v.Length, v.Count)
		if rtag != nil {
			if err = rtag.Dump(w); err != nil {
				log.Fatal(err)
			}
			tt = rtag.Tag
		}

		for _, j := range v.Tags {
			switch tt {
			case rpm.RPMTAG_HEADERSIGNATURES:
				err = j.DumpSignature(w)
			default:
				err = j.Dump(w)
			}
			fmt.Fprintln(w)
		}
		if err != nil {
			return err
		}

		if !fl {
			continue
		}

		fi, err := rpm.FileIndexHeader(v)
		if err != nil {
			return err
		}
		if err := fi.Dump(w); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("rpmdump: ")

	jd := flag.Bool("json", false, "JSON format")
	fl := flag.Bool("files", false, "Filelist from tags")
	nhdr := flag.Int("nhdr", 2, "Number of headers")

	flag.Parse()

	f := os.Stdin
	if flag.NArg() > 0 {
		fi, err := os.Open(flag.Arg(0))
		if err != nil {
			log.Fatal(err)
		}
		f = fi
	}

	buf := bufio.NewReaderSize(f, 1<<20)
	r := rpm.NewReader(buf)

	if _, err := r.Lead(); err != nil {
		log.Fatal(err)
	}
	if *nhdr < 1 {
		os.Exit(0)
	}

	var (
		hdr *rpm.Header
		h   []*rpm.Header
		err error
	)
	for i := 0; i < *nhdr; i++ {
		hdr, err = r.Next()
		if err != nil {
			break
		}
		h = append(h, hdr)
	}
	if len(h) == 0 {
		log.Fatalf("no headers: %v", err)
	}

	if *jd {
		jw := json.NewEncoder(os.Stdout)
		if err := jw.Encode(h); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}

	if err := dump(os.Stdout, *fl, h...); err != nil {
		log.Fatal(err)
	}

	if err != nil && !errors.Is(err, io.EOF) {
		log.Fatalf("error: %v", err)
	}
}
