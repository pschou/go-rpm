package main

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"reflect"
	"strings"
)

type loader interface {
	load(string, *bufio.Scanner) error
}

type str string

func (s *str) load(value string, sc *bufio.Scanner) error {
	i := strings.Index(value, "<<")
	if i == -1 {
		*(*string)(s) = strings.TrimSpace(value)
		return nil
	}
	e := value[i+2:]
	if e == "" {
		return fmt.Errorf("config/str: missing EOF")
	}
	var r []string
	for sc.Scan() {
		l := sc.Text()
		if l == e {
			break
		}
		r = append(r, l)
	}
	*(*string)(s) = strings.Join(r, "\n")
	return sc.Err()
}

type slice []string

func (s *slice) load(value string, sc *bufio.Scanner) error {
	i := strings.IndexByte(value, '{')
	if i == -1 {
		*(*[]string)(s) = strings.Fields(value)
		return nil
	}
	var r []string
	for sc.Scan() {
		l := sc.Text()
		if l == "}" {
			break
		}
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		var i int
		if i = strings.IndexByte(l, '#'); i == -1 {
			i = len(l)
		}
		r = append(r, strings.Fields(l[:i])...)
	}
	*(*[]string)(s) = r
	return sc.Err()
}

type script struct {
	data string
	prog string
}

func (s *script) load(value string, sc *bufio.Scanner) error {
	return (*str)(&s.data).load(value, sc)
}

func kp(key string) (string, string) {
	i := strings.IndexByte(key, '(')
	if i == -1 {
		return key, ""
	}
	j := strings.IndexByte(key[i:], ')')
	if j == -1 {
		return key[:i], ""
	}
	return key[:i], key[i+1 : i+j]
}

func scriptProg(key string, m map[string]loader) string {
	k, p := kp(key)
	s, ok := m[k].(*script)
	if !ok {
		return k
	}
	switch p {
	case "":
		s.prog = "/bin/sh"
	case "lua":
		s.prog = "<lua>"
	default:
		if p[0] == '/' {
			s.prog = p
			break
		}
		s.prog = path.Join("/bin", p)
	}
	return k
}

func scan1(m map[string]loader, s *bufio.Scanner) error {
	l := s.Text()
	if i := strings.IndexByte(l, '#'); i != -1 {
		l = l[:i]
	}
	if len(l) == 0 {
		return nil
	}

	i := strings.IndexAny(l, " \t")
	if i == -1 {
		return fmt.Errorf("config: invalid entry")
	}

	k := scriptProg(l[:i], m)
	ld, ok := m[k]
	if !ok {
		return fmt.Errorf("config: unknown key: %q", k)
	}
	return ld.load(l[i:], s)
}

func configMap(from interface{}) (map[string]loader, error) {
	r := make(map[string]loader)
	y := reflect.ValueOf(from).Elem()
	if y.Kind() != reflect.Struct {
		return nil, fmt.Errorf("not a struct")
	}
	t := y.Type()
	for i := 0; i < y.NumField(); i++ {
		if !y.Field(i).CanSet() {
			continue
		}
		f := t.Field(i)
		n := f.Tag.Get("name")
		if n == "" {
			n = strings.ToLower(f.Name)
		}

		switch v := y.Field(i).Addr().Interface().(type) {
		case *string:
			r[n] = (*str)(v)
		case *[]string:
			r[n] = (*slice)(v)
		case *script:
			r[n] = v
		default:
			return nil, fmt.Errorf("unknown type: %T", v)
		}
	}
	return r, nil
}

func loadconfig(r io.Reader, to interface{}) error {
	m, err := configMap(to)
	if err != nil {
		return err
	}
	s := bufio.NewScanner(r)
	for s.Scan() {
		if err := scan1(m, s); err != nil {
			return err
		}
	}
	return s.Err()
}
