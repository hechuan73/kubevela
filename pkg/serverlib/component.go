package serverlib

import (
	"context"

	"github.com/oam-dev/kubevela/pkg/server/apis"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RetrieveComponent will get component status
func RetrieveComponent(ctx context.Context, c client.Client, applicationName, componentName, namespace string) (apis.ComponentMeta, error) {
	var componentMeta apis.ComponentMeta
	// 通过k8s API获取applicationName对应的ApplicationConfiguration，然后将其中的信息转化下
	applicationMeta, err := RetrieveApplicationStatusByName(ctx, c, applicationName, namespace)
	if err != nil {
		return componentMeta, err
	}

	for _, com := range applicationMeta.Components {
		// 匹配componentName
		if com.Name != componentName {
			continue
		}
		return com, nil
	}
	return componentMeta, nil
}
