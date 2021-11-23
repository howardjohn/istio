package builder

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"golang.org/x/sync/errgroup"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cluster/nodes"
)

// Supported since kind 0.8.0 (https://github.com/kubernetes-sigs/kind/releases/tag/v0.8.0)
const clusterNameEnvKey = "KIND_CLUSTER_NAME"

// provider is an interface for kind providers to facilitate testing.
type provider interface {
	ListInternalNodes(name string) ([]nodes.Node, error)
}

// GetProvider is a variable so we can override in tests.
var GetProvider = func() provider {
	return cluster.NewProvider()
}

// Tag adds a tag to an already existent image.
func Tag(ctx context.Context, src, dest name.Tag) error {
	return onEachNode(func(n nodes.Node) error {
		cmd := n.CommandContext(ctx, "ctr", "--namespace=k8s.io", "images", "tag", "--force", src.String(), dest.String())
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to tag image: %w", err)
		}
		return nil
	})
}

// Write saves the image into the kind nodes as the given tag.
func Pull(ref name.Reference) error {
	return onEachNode(func(n nodes.Node) error {
		cmd := n.Command("crictl", "pull", ref.Name())
		// cmd := n.Command("ctr", "--namespace=k8s.io", "images", "pull", ref.Name())
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to load image to node %q: %w", n, err)
		}

		return nil
	})
}

// Write saves the image into the kind nodes as the given tag.
func TagImage(from name.Tag, dest ...name.Tag) error {
	return onEachNode(func(n nodes.Node) error {
		args := []string{"--namespace=k8s.io", "images", "tag", "--force", from.Name()}
		for _, d := range dest {
			args = append(args, d.Name())
		}
		cmd := n.Command("ctr", args...)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to load image to node %q: %w", n, err)
		}

		return nil
	})
}

// Write saves the image into the kind nodes as the given tag.
func Write(ctx context.Context, tag name.Tag, img v1.Image) error {
	return onEachNode(func(n nodes.Node) error {
		pr, pw := io.Pipe()

		grp := errgroup.Group{}
		grp.Go(func() error {
			return pw.CloseWithError(tarball.Write(tag, img, pw))
		})

		cmd := n.CommandContext(ctx, "ctr", "--namespace=k8s.io", "images", "import", "-").SetStdin(pr)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to load image to node %q: %w", n, err)
		}

		if err := grp.Wait(); err != nil {
			return fmt.Errorf("failed to write intermediate tarball representation: %w", err)
		}

		return nil
	})
}

// onEachNode executes the given function on each node. Exits on first error.
func onEachNode(f func(nodes.Node) error) error {
	nodeList, err := getNodes()
	if err != nil {
		return err
	}

	for _, n := range nodeList {
		if err := f(n); err != nil {
			return err
		}
	}
	return nil
}

// getNodes gets all the nodes of the default cluster.
// Returns an error if none were found.
func getNodes() ([]nodes.Node, error) {
	provider := GetProvider()

	clusterName := os.Getenv(clusterNameEnvKey)
	if clusterName == "" {
		clusterName = cluster.DefaultName
	}

	nodeList, err := provider.ListInternalNodes(clusterName)
	if err != nil {
		return nil, err
	}
	if len(nodeList) == 0 {
		return nil, fmt.Errorf("no nodes found for cluster %q", cluster.DefaultName)
	}

	return nodeList, nil
}
