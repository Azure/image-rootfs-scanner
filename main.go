package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"text/template"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/platforms"
	"github.com/containerd/containerd/remotes/docker"
	"github.com/containerd/continuity"
	"github.com/cpuguy83/dockercfg"
	"github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"golang.org/x/sync/semaphore"
)

func main() {
	flags := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ExitOnError)

	home, _ := os.UserHomeDir()
	if home == "" {
		home = "~/" // Using this for display purposes, if we need home and did not get one we'll error out later
	}

	var (
		sockPath      = envOrDefault("CONTAINERD_ADDRESS", "/run/containerd/containerd.sock")
		root          = filepath.Join(home, filepath.Base(os.Args[0]))
		ns            = envOrDefault("CONTAINERD_NAMESPACE", filepath.Base(os.Args[0]))
		workers       = runtime.GOMAXPROCS(0)
		debug         bool
		format        = envOrDefault("OUTPUT_FORMAT", "{{.}}")
		platform      = platforms.DefaultString()
		allowPlainTTP bool
		matchPattern  = "/bin/(sh|bash|ssh|curl|wget|nc|csh|zsh|fish)$"
	)

	flags.StringVar(&sockPath, "containerd", sockPath, "path to containerd socket to use as storage backend")
	flags.StringVar(&root, "root", root, "Directory to store data such as images and mounts when in standalone(no containerd) mode")
	flags.StringVar(&ns, "namespace", ns, "namespace for containerd content")
	flags.IntVar(&workers, "workers", workers, "Set the number of simultaneous images to work on")
	flags.BoolVar(&debug, "debug", debug, "enable debug mode")
	flags.StringVar(&format, "format", format, "set the template to use for the result")
	flags.StringVar(&platform, "platform", platform, "specify platform for image to pull")
	flags.BoolVar(&allowPlainTTP, "plain-http", allowPlainTTP, "Allow plain HTTP for registry requests")
	flags.StringVar(&matchPattern, "pattern", matchPattern, "regexp pattern to match file paths")

	var args []string
	if len(os.Args) > 1 {
		args = os.Args[1:]
	}
	if err := flags.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "error parsing flags", err)
		os.Exit(1)
	}

	if home == "~/" && !flags.Lookup(root).Changed && sockPath == "" {
		fmt.Fprintln(os.Stderr, "Could not determine home dir for data storage.")
		fmt.Fprintln(os.Stderr, "Either use containerd or specify a custom --root")
		os.Exit(1)
	}

	tmpl := template.Must(template.New("report").Parse(format))

	if flags.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "requires at least 1 argument")
		os.Exit(1)
	}

	if debug {
		logrus.SetLevel(logrus.DebugLevel)
	} else {
		logrus.SetLevel(logrus.WarnLevel)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := os.MkdirAll(root, 0700); err != nil {
		fmt.Fprintln(os.Stderr, "error creating root directory:", err)
		os.Exit(1)
	}

	client, snapshotter, err := newClient(ctx, sockPath, filepath.Join(root, "data"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error configuring client:", err)
		os.Exit(1)
	}

	client.NamespaceService().Create(ctx, ns, nil) //nolint:errcheck
	ctx = namespaces.WithNamespace(ctx, ns)

	sem := semaphore.NewWeighted(int64(workers))
	var wg sync.WaitGroup
	defer wg.Wait()

	resolver := docker.NewResolver(docker.ResolverOptions{
		Hosts: docker.ConfigureDefaultRegistries(
			docker.WithPlainHTTP(func(string) (bool, error) {
				return allowPlainTTP, nil
			}),
			docker.WithAuthorizer(docker.NewDockerAuthorizer(
				docker.WithAuthCreds(dockercfg.GetRegistryCredentials),
			))),
	})

	matcher := regexp.MustCompile(matchPattern)

	for _, ref := range flags.Args() {
		wg.Add(1)
		if err := sem.Acquire(ctx, 1); err != nil {
			panic(err)
		}
		go func(ref string) {
			defer wg.Done()
			defer sem.Release(1)

			ctx := log.WithLogger(ctx, log.G(ctx).WithField("ref", ref))
			r := result{Ref: ref}

			m, err := buildManifest(ctx, client, ref, snapshotter, platform, resolver)
			if err != nil {
				r.Err = err
			} else {
				r.manifest = m

				// So we don't have to allocate a new slice for every invocation of our
				// walk function.
				singlePath := make([]string, 1)

				err := walkManifest(ctx, m, func(ctx context.Context, res Resource) error {
					if res.Mode().IsDir() {
						return nil
					}
					var paths []string
					if h, ok := res.(continuity.Hardlinkable); ok {
						paths = h.Paths()
					} else {
						singlePath[0] = res.Path()
						paths = singlePath
					}

					for _, p := range paths {
						if matcher.MatchString(p) {
							r.Found = append(r.Found, p)
						}
					}
					return nil
				})
				if err != nil {
					r.Err = err
				}
			}

			buf := bytes.NewBuffer(nil)
			if err := tmpl.Execute(buf, r); err != nil {
				panic(err)
			}
			fmt.Fprintln(os.Stdout, buf)
		}(ref)
	}
}

func envOrDefault(varName, defaultValue string) string {
	if v, ok := os.LookupEnv(varName); ok {
		return v
	}
	return defaultValue
}
