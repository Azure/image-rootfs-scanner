module github.com/Azure/image-rootfs-scanner

go 1.16

require (
	github.com/containerd/containerd v1.5.5
	github.com/containerd/continuity v0.3.0
	github.com/cpuguy83/dockercfg v0.3.1
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.0.2
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.9.2
	github.com/spf13/pflag v1.0.5
	go.etcd.io/bbolt v1.3.6
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
)

replace github.com/containerd/containerd => github.com/cpuguy83/containerd v0.0.0-20210712191206-cbdebd18eb69
