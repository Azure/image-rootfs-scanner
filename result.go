package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/template"

	"github.com/containerd/containerd/log"
	"github.com/containerd/continuity"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

type result struct {
	Found    []string
	Ref      string
	Err      error
	manifest *continuity.Manifest
}

// resource provides a concrete type to convert a continuity.Resource to when
// executing match templates.
// See `scan` for usage.
type resource struct {
	Digests  []digest.Digest   `json:"digests"`
	UID      int64             `json:"uid"`
	GID      int64             `json:"gid"`
	FileMode os.FileMode       `json:"file_mode"`
	Path     string            `json:"path"`
	Size     int64             `json:"size"`
	XAttrs   map[string][]byte `json:"xattrs"`
}

func (r result) Status() string {
	switch {
	case r.Err != nil:
		return "ERROR"
	case len(r.Found) > 0:
		return "MATCH"
	default:
		return "NONE"
	}
}

func (r result) HasMatches() bool {
	return len(r.Found) > 0
}

func (r result) HasError() bool {
	return r.Err != nil
}

func (r result) ManifestProto() string {
	b, err := continuity.Marshal(r.manifest)
	if err != nil {
		return err.Error()
	}
	return string(b)
}

func (r result) Manifest() string {
	if r.Err != nil {
		return r.Err.Error()
	}
	buf := bytes.NewBuffer(nil)
	if err := continuity.MarshalText(buf, r.manifest); err != nil {
		return errors.Wrap(err, "error marshalling manifest").Error()
	}
	return buf.String()
}

func (r result) Data() string {
	if r.Err != nil {
		return fmt.Sprintf(`{"error": "%s"}`, r.Err)
	}

	b := &strings.Builder{}
	b.WriteString(`[`)
	for i, found := range r.Found {
		b.WriteString(`"` + found + `"`)
		if i < len(r.Found)-1 {
			b.WriteString(",")
		}
	}
	b.WriteString("]")

	return b.String()
}

func (r result) String() string {
	return fmt.Sprintf("%s %s %s", r.Ref, r.Status(), r.Data())
}

func scan(ctx context.Context, r *result, tmpl *template.Template) error {
	m := r.manifest

	// So we don't have to allocate a new slice for every invocation of our
	// walk function.
	singlePath := make([]string, 1)
	buf := bytes.NewBuffer(make([]byte, 5))

	return walkManifest(ctx, m, func(ctx context.Context, res continuity.Resource) error {
		tR := &resource{
			UID:      res.UID(),
			GID:      res.GID(),
			FileMode: res.Mode(),
		}

		if reg, ok := res.(continuity.RegularFile); ok {
			tR.Digests = reg.Digests()
			tR.XAttrs = reg.XAttrs()
			tR.Size = reg.Size()
		}

		var paths []string
		if h, ok := res.(continuity.Hardlinkable); ok {
			paths = h.Paths()
		} else {
			singlePath[0] = res.Path()
			paths = singlePath
		}

		for _, p := range paths {
			buf.Reset()
			tR.Path = p
			if err := tmpl.Execute(buf, tR); err != nil {
				return err
			}

			log.G(ctx).WithField("path", p).Debugf(buf.String())
			ok, err := strconv.ParseBool(strings.TrimSpace(buf.String()))
			if err != nil {
				return errors.Wrap(err, "template returned something other than a bool value")
			}

			if ok {
				r.Found = append(r.Found, p)
			}
		}
		return nil
	})
}
