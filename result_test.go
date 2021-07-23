package main

import (
	"context"
	"os"
	"testing"
	"text/template"

	"github.com/containerd/continuity"
)

type testResource struct {
	path string
	uid  int64
	gid  int64
	mode os.FileMode
}

func (r *testResource) Path() string {
	return r.path
}

func (r *testResource) UID() int64 {
	return r.uid
}

func (r *testResource) GID() int64 {
	return r.gid
}

func (r *testResource) Mode() os.FileMode {
	return r.mode
}

type testResourceLink struct {
	continuity.Resource
	paths []string
}

func (r *testResourceLink) Paths() []string {
	ls := make([]string, len(r.paths))
	copy(ls, r.paths)
	return ls
}

func TestScan(t *testing.T) {
	ctx := context.Background()

	m := &continuity.Manifest{
		Resources: []continuity.Resource{
			&testResource{path: "/foo", mode: 0600},
			&testResourceLink{
				Resource: &testResource{path: "/bar", mode: 0600},
				paths:    []string{"/baz", "/quux"},
			},
			&testResource{path: "/someDir", mode: 0755 | os.ModeDir},
			&testResource{path: "/someDir/someFile", mode: 0644},
		},
	}

	t.Run("Find normal resource by path", func(t *testing.T) {
		r := &result{manifest: m}
		tmpl := template.Must(template.New(t.Name()).Parse("{{ eq .Path \"/foo\"}}"))
		if err := scan(ctx, r, tmpl); err != nil {
			t.Fatal(err)
		}
		if len(r.Found) != 1 {
			t.Fatalf("expected 1 found resource, got %d,: %v", len(r.Found), r.Found)
		}
		if r.Found[0] != "/foo" {
			t.Fatalf("expected found resource to be /foo, got: %s", r.Found[0])
		}
	})

	t.Run("Find linked resource by path", func(t *testing.T) {
		r := &result{manifest: m}
		tmpl := template.Must(template.New(t.Name()).Parse("{{ eq .Path \"/quux\"}}"))
		if err := scan(ctx, r, tmpl); err != nil {
			t.Fatal(err)
		}
		if len(r.Found) != 1 {
			t.Fatalf("expected 1 found resource, got %d: %v", len(r.Found), r.Found)
		}
		if r.Found[0] != "/quux" {
			t.Fatalf("expected found resource to be /quux, got: %s", r.Found[0])
		}
	})
	t.Run("Find directory by mode", func(t *testing.T) {
		r := &result{manifest: m}
		tmpl := template.Must(template.New(t.Name()).Parse("{{ .FileMode.IsDir }}"))
		if err := scan(ctx, r, tmpl); err != nil {
			t.Fatal(err)
		}
		if len(r.Found) != 1 {
			t.Fatalf("expected 1 found resource, got %d: %v", len(r.Found), r.Found)
		}
		if r.Found[0] != "/someDir" {
			t.Fatalf("expected found resource to be /someDir, got: %s", r.Found[0])
		}
	})
}
