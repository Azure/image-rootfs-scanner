package main

import (
	"context"
	"io/ioutil"
	"os"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/remotes"
	"github.com/containerd/continuity"
	"github.com/opencontainers/image-spec/identity"
	"github.com/pkg/errors"
)

// Including these for the sake of brevity
type (
	Manifest      = continuity.Manifest
	Resource      = continuity.Resource
	Client        = containerd.Client
	Image         = containerd.Image
	Resolver      = remotes.Resolver
	RemoteContext = containerd.RemoteContext
)

// buildManifest pulls the image and creates a continuity manifest
func buildManifest(ctx context.Context, client *Client, ref, snapshotter, platform string, resolver Resolver) (*Manifest, error) {
	log.G(ctx).Debug("Pulling image")

	img, err := client.Pull(ctx, ref, containerd.WithPullUnpack, containerd.WithPlatform(platform), func() containerd.RemoteOpt {
		if snapshotter == "" {
			return func(*Client, *RemoteContext) error {
				return nil
			}
		}
		return containerd.WithPullSnapshotter(snapshotter)
	}(), containerd.WithResolver(resolver))
	if err != nil {
		return nil, errors.Wrap(err, "error pulling image")
	}

	var manifest *Manifest
	err = do(ctx, client, img, snapshotter, func(ctx context.Context, p string) error {
		fs, err := continuity.NewContext(p)
		if err != nil {
			return errors.Wrap(err, "error creating filesystem context")
		}
		manifest, err = continuity.BuildManifest(fs)
		return err
	})
	return manifest, err
}

func walkManifest(ctx context.Context, m *Manifest, f func(context.Context, Resource) error) error {
	for _, r := range m.Resources {
		r := r
		if err := f(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

// do prepares the passed in images rootfs and calls `f` with the path of the rootfs.
// The rootfs and all assoicated resources are cleaned up at the end of this function.
func do(ctx context.Context, client *Client, img Image, snapshotter string, f func(context.Context, string) error) error {
	diffIDs, err := img.RootFS(ctx)
	if err != nil {
		return errors.Wrap(err, "error getting rootfs layer entries for image")
	}
	chainID := identity.ChainID(diffIDs).String()
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("image.ChainID", chainID))

	target, err := ioutil.TempDir("", chainID)
	if err != nil {
		return errors.Wrap(err, "error creating mount target")
	}
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("mount.Target", target))

	defer os.RemoveAll(target)

	log.G(ctx).Debug("Creating snapshot")
	mounts, err := client.SnapshotService(snapshotter).View(ctx, target, chainID)
	if err != nil {
		if errdefs.IsAlreadyExists(err) {
			mounts, err = client.SnapshotService(snapshotter).Mounts(ctx, target)
			err = errors.Wrap(err, "error getting snapshot mounts")
		}
		if err != nil {
			return errors.Wrap(err, "error getting mounts")
		}
	}
	defer client.SnapshotService(snapshotter).Remove(ctx, target) //nolint:errcheck

	log.G(ctx).Debug("Mounting image rootfs")
	if err := mount.All(mounts, target); err != nil {
		return errors.Wrap(err, "error mounting rootfs")
	}
	defer func() {
		if err := mount.UnmountAll(target, 0); err != nil {
			log.G(ctx).WithError(err).Error("error unmounting image")
		}
	}()

	return f(ctx, target)
}
