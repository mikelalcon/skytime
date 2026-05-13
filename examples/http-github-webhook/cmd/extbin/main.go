// extbin: http-github-webhook example custom CLI; see pkg/cli for the build-your-own-binary pattern.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	skygh "github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/github"
	skyweb "github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/webhook"
	"github.com/mikelalcon/skytime/pkg/cli"
	skycore "github.com/mikelalcon/skytime/pkg/extension/builtin/core"
	skyhttp "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	root, err := cli.NewRootCommand(cli.WithExtensions(skyhttp.New(), skycore.New(), skygh.New(), skyweb.New()), cli.WithCredfile(os.Getenv(cli.EnvCredfilePath)))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := root.ExecuteContext(ctx); err != nil {
		cli.RenderRootError(os.Stderr, err)
		os.Exit(1)
	}
}
