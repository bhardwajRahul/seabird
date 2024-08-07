package extension

import (
	"context"
	"fmt"
	"strings"

	"github.com/getseabird/seabird/api"
	"github.com/getseabird/seabird/internal/util"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	Extensions = append(Extensions, NewNetworking)
}

func NewNetworking(_ context.Context, cluster *api.Cluster) (Extension, error) {
	return &Networking{Cluster: cluster}, nil
}

type Networking struct {
	Noop
	*api.Cluster
}

func (e *Networking) CreateColumns(ctx context.Context, resource *metav1.APIResource, columns []api.Column) []api.Column {
	switch util.GVRForResource(resource).String() {
	case networkingv1.SchemeGroupVersion.WithResource("ingresses").String():
		columns = append(columns,
			api.Column{
				Name:     "Hosts",
				Priority: 70,
				Bind: func(cell api.Cell, object client.Object) {
					ingress := object.(*networkingv1.Ingress)
					var hosts []string
					for _, r := range ingress.Spec.Rules {
						hosts = append(hosts, r.Host)
					}
					cell.SetLabel(strings.Join(hosts, ", "))
				},
			},
		)
	}
	return columns
}

func (e *Networking) CreateObjectProperties(ctx context.Context, _ *metav1.APIResource, object client.Object, props []api.Property) []api.Property {
	switch object := object.(type) {
	case *networkingv1.Ingress:
		rules := &api.GroupProperty{Name: "Rules"}
		for _, r := range object.Spec.Rules {
			var paths []api.Property
			for _, p := range r.HTTP.Paths {
				paths = append(paths, &api.TextProperty{
					Name:  fmt.Sprintf("%s %s", ptr.Deref(p.PathType, ""), p.Path),
					Value: fmt.Sprintf("%s:%s%d", p.Backend.Service.Name, p.Backend.Service.Port.Name, p.Backend.Service.Port.Number),
				})
			}
			rules.Children = append(rules.Children, &api.GroupProperty{
				Name:     r.Host,
				Children: paths,
			})
		}
		props = append(props, rules)
	}

	return props
}
