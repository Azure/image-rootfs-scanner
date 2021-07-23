package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/containerd/continuity"
	"github.com/pkg/errors"
)

type result struct {
	Found    []string
	Ref      string
	Err      error
	manifest *continuity.Manifest
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
