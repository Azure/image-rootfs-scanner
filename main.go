package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"text/template"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/platforms"
	"github.com/containerd/containerd/remotes/docker"
	"github.com/cpuguy83/dockercfg"
	"github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"golang.org/x/sync/semaphore"
)

var (
	// specifies which paths to recrusively look for specified files
	targetPaths = []string{}
	targetBins  = []string{"sh", "bash", "ssh", "curl", "wget", "nc", "csh", "zsh", "fish"}
)

func main() {
	flags := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ExitOnError)

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not determine user home directory:", err)
		os.Exit(1)
	}

	var (
		sockPath      = envOrDefault("CONTAINERD_ADDRESS", "/run/containerd/containerd.sock")
		root          = filepath.Join(home, filepath.Base(os.Args[0]))
		ns            = envOrDefault("CONTAINERD_NAMESPACE", filepath.Base(os.Args[0]))
		workers       = runtime.GOMAXPROCS(0)
		debug         bool
		format        = envOrDefault("OUTPUT_FORMAT", "{{ .Ref }}{{if .HasError }} ERROR {{.Data}}{{else}}{{if .HasEntries }} NOTOK {{ .Data }}{{ else }} OK {{end}}{{end}}\n")
		platform      = platforms.DefaultString()
		allowPlainTTP bool
	)

	flags.StringVar(&sockPath, "containerd", sockPath, "path to containerd socket to use as storage backend")
	flags.StringVar(&root, "root", root, "Directory to store data such as images and mounts when in standalone(no containerd) mode")
	flags.StringVar(&ns, "namespace", ns, "namespace for containerd content")
	flags.StringSliceVar(&targetPaths, "target", targetPaths, "target paths to search for files, otherwise the full iamge will be searched")
	flags.StringSliceVar(&targetBins, "target-bins", targetBins, "target binaries to search for")
	flags.IntVar(&workers, "workers", workers, "Set the number of simultaneous images to work on")
	flags.BoolVar(&debug, "debug", debug, "enable debug mode")
	flags.StringVar(&format, "format", format, "set the template to use for the result")
	flags.StringVar(&platform, "platform", platform, "specify platform for image to pull")
	flags.BoolVar(&allowPlainTTP, "plain-http", allowPlainTTP, "Allow plain HTTP for registry requests")

	var args []string
	if len(os.Args) > 1 {
		args = os.Args[1:]
	}
	if err := flags.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "error parsing flags", err)
		os.Exit(1)
	}

	tmpl := template.Must(template.New("report").Parse(format))

	if flags.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "requires at least 1 argument")
		os.Exit(1)
	}

	if len(targetPaths) == 0 {
		if targetsEnv := os.Getenv("TARGET_PATHS"); targetsEnv != "" {
			targetPaths = filepath.SplitList(targetsEnv)
		}
		if len(targetBins) == 0 {
			fmt.Fprintln(os.Stderr, "error: no target binaries specified")
			os.Exit(1)
		}
	}
	if len(targetBins) == 0 {
		if targetBinsEnv := os.Getenv("TARGET_BINS"); targetBinsEnv != "" {
			targetBins = strings.Split(targetBinsEnv, " ")
		}
		targetPaths = []string{"/"}
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

	client.NamespaceService().Create(ctx, ns, nil)
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

	for _, ref := range flags.Args() {
		wg.Add(1)
		sem.Acquire(ctx, 1)
		go func(ref string) {
			defer wg.Done()
			defer sem.Release(1)

			report, err := run(ctx, client, ref, snapshotter, containerd.WithPlatform(platform), func() containerd.RemoteOpt {
				if snapshotter == "" {
					return func(*containerd.Client, *containerd.RemoteContext) error {
						return nil
					}
				}
				return containerd.WithPullSnapshotter(snapshotter)
			}(), containerd.WithResolver(resolver),

			buf := bytes.NewBuffer(nil)
			if err := tmpl.Execute(buf, result{Found: report, Err: err, Ref: ref}); err != nil {
				panic(err)
			}
			io.Copy(os.Stdout, buf)
		}(ref)
	}
}

func envOrDefault(varName, defaultValue string) string {
	if v, ok := os.LookupEnv(varName); ok {
		return v
	}
	return defaultValue
}

type result struct {
	Found []string
	Ref   string
	Err   error
}

func (r result) Status() string {
	switch {
	case r.Err != nil:
		return "ERROR"
	case len(r.Found) == 0:
		return "FOUND"
	default:
		return "NONE"
	}
}

func (r result) HasEntries() bool {
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
