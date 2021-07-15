package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/content/local"
	"github.com/containerd/containerd/diff"
	"github.com/containerd/containerd/diff/apply"
	"github.com/containerd/containerd/diff/walking"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/metadata"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/containerd/snapshots/native"
	"github.com/containerd/containerd/snapshots/overlay"
	"go.etcd.io/bbolt"
)

func newClient(ctx context.Context, sock, root string) (*containerd.Client, string, error) {
	var (
		client      *containerd.Client
		snapshotter string
		err         error
	)
	if sock == "" {
		var services containerd.ClientOpt
		services, snapshotter, err = makeServices(ctx, root)
		if err != nil {
			return nil, "", err
		}
		client, err = containerd.New("", services)
	} else {
		client, err = containerd.New(sock)
	}
	return client, snapshotter, err
}

func makeServices(ctx context.Context, root string) (containerd.ClientOpt, string, error) {
	var ss string

	cs, err := local.NewStore(filepath.Join(root, "content"))
	if err != nil {
		return nil, "", err
	}

	db, err := bbolt.Open(filepath.Join(root, "metadata.db"), 0600, &bbolt.Options{
		Timeout: 10 * time.Second,
	})
	if err != nil {
		return nil, "", err
	}

	snapshotters := map[string]snapshots.Snapshotter{}
	s, err := overlay.NewSnapshotter(filepath.Join(root, "overlay"))
	if err != nil {
		log.G(ctx).WithError(err).Debug("Error initializing overlay snapshotter")
		log.G(ctx).Debug("Falling back to native snapshotter")

		s, err := native.NewSnapshotter(filepath.Join(root, "native"))
		if err != nil {
			return nil, "", fmt.Errorf("error creating fallback snapshotter: %w", err)
		}
		snapshotters["native"] = s
		ss = "native"
	} else {
		log.G(ctx).Debug("Using overlay snapshotter")
		snapshotters["overlay"] = s
		ss = "overlay"
	}

	mdb := metadata.NewDB(db, cs, snapshotters)
	return containerd.WithServices(
		containerd.WithContentStore(mdb.ContentStore()),
		containerd.WithSnapshotters(mdb.Snapshotters()),
		containerd.WithLeasesService(metadata.NewLeaseManager(mdb)),
		containerd.WithDiffService(&diffService{
			Comparer: walking.NewWalkingDiff(mdb.ContentStore()),
			Applier:  apply.NewFileSystemApplier(mdb.ContentStore()),
		}),
		containerd.WithImageStore(metadata.NewImageStore(mdb)),
		containerd.WithNamespaceService(&namespaceStore{mdb}),
	), ss, nil
}

type diffService struct {
	diff.Comparer
	diff.Applier
}

type namespaceStore struct {
	db *metadata.DB
}

func (s *namespaceStore) Create(ctx context.Context, namespace string, labels map[string]string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return metadata.NewNamespaceStore(tx).Create(ctx, namespace, labels)
	})
}

func (s *namespaceStore) Labels(ctx context.Context, namespace string) (labels map[string]string, err error) {
	err = s.db.View(func(tx *bbolt.Tx) error {
		labels, err = metadata.NewNamespaceStore(tx).Labels(ctx, namespace)
		return err
	})
	return
}

func (s *namespaceStore) SetLabel(ctx context.Context, namespace, key, value string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return metadata.NewNamespaceStore(tx).SetLabel(ctx, namespace, key, value)
	})
}

func (s *namespaceStore) List(ctx context.Context) (ls []string, err error) {
	err = s.db.View(func(tx *bbolt.Tx) error {
		ls, err = metadata.NewNamespaceStore(tx).List(ctx)
		return err
	})
	return
}

func (s *namespaceStore) Delete(ctx context.Context, namespace string, opts ...namespaces.DeleteOpts) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return metadata.NewNamespaceStore(tx).Delete(ctx, namespace, opts...)
	})
}
