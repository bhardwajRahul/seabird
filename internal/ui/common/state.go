package common

import (
	"context"

	"github.com/getseabird/seabird/api"
	"github.com/getseabird/seabird/extension"
	"github.com/getseabird/seabird/internal/ctxt"
	"github.com/go-logr/logr"
	"github.com/imkira/go-observer/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type State struct {
	Preferences observer.Property[api.Preferences]
}

type ClusterState struct {
	*State
	*api.Cluster
	Extensions       []extension.Extension
	Namespaces       observer.Property[[]*corev1.Namespace]
	SelectedResource observer.Property[*metav1.APIResource]
	SearchText       observer.Property[string]
	SearchFilter     observer.Property[SearchFilter]
	SelectedObject   observer.Property[client.Object]
}

func NewState() (*State, error) {
	prefs, err := api.LoadPreferences()
	if err != nil {
		return nil, err
	}
	prefs.Defaults()

	return &State{
		Preferences: observer.NewProperty(*prefs),
	}, nil
}

func (s *State) NewClusterState(ctx context.Context, clusterPrefs observer.Property[api.ClusterPreferences]) (*ClusterState, error) {
	logf.SetLogger(logr.Discard())

	clusterApi, err := api.NewCluster(ctx, clusterPrefs)
	if err != nil {
		return nil, err
	}
	ctx = ctxt.With[*api.Cluster](ctx, clusterApi)

	cluster := ClusterState{
		State:            s,
		Cluster:          clusterApi,
		Namespaces:       observer.NewProperty([]*corev1.Namespace{}),
		SelectedResource: observer.NewProperty[*metav1.APIResource](nil),
		SearchText:       observer.NewProperty(""),
		SearchFilter:     observer.NewProperty(SearchFilter{}),
		SelectedObject:   observer.NewProperty[client.Object](nil),
	}

	var ns *metav1.APIResource
	for _, r := range clusterApi.Resources {
		if r.Group == corev1.SchemeGroupVersion.Group && r.Version == corev1.SchemeGroupVersion.Version && r.Name == "namespaces" {
			ns = &r
			break
		}
	}
	api.Watch(ctx, clusterApi, ns, api.WatchOptions[*corev1.Namespace]{Property: cluster.Namespaces})

	for _, new := range extension.Extensions {
		cluster.Extensions = append(cluster.Extensions, new(clusterApi))
	}

	return &cluster, nil
}
