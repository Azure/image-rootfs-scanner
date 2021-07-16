# image-rootfs-scanner

A tool to pull and scan the rootfs of any container image for different
binaries.  It started out as a means of finding "restricted" binaries, e.g.
binaries that we'd prefer not be in an image, such as shells, curl, etc, hence
the defaults.

This project should only be used to scan *trusted* images. It is not designed
to find things that someone is trying to hide, just as a safeguard to find
things that were forgotten.

Note, this does not scan individual layers, only the final rootfs. Again, the
intent is to limit what is available inside a container, not to prevent a
binary from being on the host machine.

Out of the box it depends on a containerd daemon running to connect to, however
you can configure it to run stand-alone with data being stored in the specified
root location (default is in your home dir).

It currently requires a user with CAP_SYS_ADMIN (e.g. `root`) in order to mount
things. I do have some work done to allow this to run as an unprivileged user
and mount using fuse-overlayfs, but there is still some work to do to make that
happen.

## Usage

This assumes you have containerd up and running with the default socket location (`/run/containerd/containerd.sock`).
You must provide the full canonical URL to an image, e.g. instead of `ubuntu` => `docker.io/library/ubuntu:latest`.
It outputs a report which you can customize if needed (see the `--format` flag).

```
$ sudo image-rootfs-scanner docker.io/library/busybox:latest
docker.io/library/busybox:latest MATCH ["nc","sh","wget"]
```

The report has three columns, the image ref, the status, and a json encoded value of the data.
If there is an error it will be in the report, example for running without the needed privileges:

```
docker.io/library/busybox:latest ERROR {"error": "error mounting rootfs: operation not permitted"}
```

The available template fields for the report (available to `--format`) are:

- .Ref - The image reference used to fetch the image
- .Found - An array of found binaries
- .Status - Strinified status. `ERROR` if there was an error, `MATCH` there are found binaries, and `NONE` for no matches.
- .Data - json representation of the data, such as an error or the matched binaries.
- .HasMatches - Boolean for `len(found) > -1`
- .HasError - Boolean for if there is an error

By default this scans for several different things such as `bash`, `zsh`,
`ssh`, `curl`, and more. See `--help` for the full list.
You can also customize it to only look in a specific location instead of `/` recursively.

## Building

*The tool is only able to work on Linux for now*

Requires go with go modules support, then `go build`
You can also build with Docker:

```
$ docker build --output=bin/ --platform=local .
```

or

```
$ docker build --output=bin/ --platform=local https://github.com/Azure/image-rootfs-scanner.git
```

## Contributing

This project welcomes contributions and suggestions.  Most contributions require you to agree to a
Contributor License Agreement (CLA) declaring that you have the right to, and actually do, grant us
the rights to use your contribution. For details, visit https://cla.opensource.microsoft.com.

When you submit a pull request, a CLA bot will automatically determine whether you need to provide
a CLA and decorate the PR appropriately (e.g., status check, comment). Simply follow the instructions
provided by the bot. You will only need to do this once across all repos using our CLA.

This project has adopted the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/).
For more information see the [Code of Conduct FAQ](https://opensource.microsoft.com/codeofconduct/faq/) or
contact [opencode@microsoft.com](mailto:opencode@microsoft.com) with any additional questions or comments.

## Trademarks

This project may contain trademarks or logos for projects, products, or services. Authorized use of Microsoft 
trademarks or logos is subject to and must follow 
[Microsoft's Trademark & Brand Guidelines](https://www.microsoft.com/en-us/legal/intellectualproperty/trademarks/usage/general).
Use of Microsoft trademarks or logos in modified versions of this project must not cause confusion or imply Microsoft sponsorship.
Any use of third-party trademarks or logos are subject to those third-party's policies.
