FROM --platform=$BUILDPLATFORM golang:1.16 AS build
WORKDIR /build
COPY go.* .
ARG TARGETOS
ARG TARGETARCH
ENV GOOS=$TARGETOS
ENV GOARCH=$TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG GOCACHE=/root/.gocache
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.gocache go build -o rootfs-scan

FROM buildpack-deps AS driver
ARG FUSE_OVERLAY_VERSION=v1.6
RUN curl -SLf https://github.com/containers/fuse-overlayfs/releases/download/${FUSE_OVERLAY_VERSION}/fuse-overlayfs-x86_64 > /opt/fuse-overlayfs
RUN chmod +x /opt/fuse-overlayfs

# Note this image doesn't really work yet due to some mounting issues in the
# core containerd libs that we'll need to work around.
FROM ubuntu:20.04 AS rootless-img
RUN apt-get update && apt-get install -y fuse3 ca-certificates pigz
COPY --from=build /build/rootfs-scan /usr/bin/
COPY --from=driver /opt/fuse-overlayfs /usr/bin/
RUN mkdir -p /var/lib/rootfsscan && chown -R nobody /var/lib/rootfsscan
VOLUME /var/lib/rootfsscan
USER nobody
ENTRYPOINT ["/usr/bin/rootfs-scan", "--root=/var/lib/rootfsscan"]

FROM scratch
COPY --from=build /build/rootfs-scan /image-rootfs-scan
