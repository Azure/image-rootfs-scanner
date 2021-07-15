package main

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/mount"
	"github.com/opencontainers/image-spec/identity"
	"github.com/pkg/errors"
)

func run(ctx context.Context, client *containerd.Client, ref, snapshotter string, opts ...containerd.RemoteOpt) ([]string, error) {
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("ref", ref))
	log.G(ctx).Debug("Pulling image")

	opts = append(opts, containerd.WithPullUnpack)
	img, err := client.Pull(ctx, ref, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "error pulling image")
	}

	diffIDs, err := img.RootFS(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "error getting rootfs layer entries for image")
	}
	chainID := identity.ChainID(diffIDs).String()
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("image.ChainID", chainID))

	target, err := ioutil.TempDir("", chainID)
	if err != nil {
		return nil, errors.Wrap(err, "error creating mount target")
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
			return nil, errors.Wrap(err, "error getting mounts")
		}
	}
	defer client.SnapshotService(snapshotter).Remove(ctx, target)

	log.G(ctx).Debug("Mounting image rootfs")
	if err := mount.All(mounts, target); err != nil {
		return nil, errors.Wrap(err, "error mounting rootfs")
	}
	defer func() {
		if err := mount.UnmountAll(target, 0); err != nil {
			log.G(ctx).WithError(err).Error("error unmounting image")
		}
	}()

	if len(targetPaths) == 0 {
		targetPaths = append(targetPaths, "/")
	}

	var found []string
	log.G(ctx).Debug("Starting image scan")
	for _, scanPath := range targetPaths {
		err := filepath.WalkDir(filepath.Join(target, scanPath), func(p string, info os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			for _, bin := range targetBins {
				if filepath.Base(p) == bin {
					found = append(found, bin)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return found, nil
}
