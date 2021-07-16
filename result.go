package main

import (
	"fmt"
	"strings"
)

type result struct {
	Found []string
	Ref   string
	Err   error
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
