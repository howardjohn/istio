package main

import (
	"context"
	"io"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

type ResourceLogger interface {
	Resource
	IsLogger()
}

func GetLogs[T ResourceLogger](c *Client, name, namespace string, opts v1.PodLogOptions) (string, error) {
	result := new(T)
	gv := (*result).ResourceMetadata()
	res, err := c.client.Get().
		Namespace(namespace).
		Resource((*result).ResourceName()).
		SubResource("log").
		Name(name).
		VersionedParams(&opts, scheme.ParameterCodec).
		AbsPath(defaultPath(gv)).
		Stream(context.Background())
	if err != nil {
		return "", nil
	}
	defer func() {
		res.Close()
	}()

	builder := &strings.Builder{}
	if _, err = io.Copy(builder, res); err != nil {
		return "", err
	}

	return builder.String(), nil
}
