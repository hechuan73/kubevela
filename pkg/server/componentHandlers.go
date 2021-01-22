package server

import (
	"os"

	"github.com/gin-gonic/gin"

	cmdutil "github.com/oam-dev/kubevela/pkg/commands/util"
	"github.com/oam-dev/kubevela/pkg/server/util"
	"github.com/oam-dev/kubevela/pkg/serverlib"
	"github.com/oam-dev/kubevela/pkg/utils/env"
)

// GetComponent gets a comoponent from cluster
func (s *APIServer) GetComponent(c *gin.Context) {
	envName := c.Param("envName")
	envMeta, err := env.GetEnvByName(envName)
	if err != nil {
		util.HandleError(c, util.StatusInternalServerError, err)
		return
	}
	namespace := envMeta.Namespace
	applicationName := c.Param("appName")
	componentName := c.Param("compName")
	ctx := util.GetContext(c)
	// 通过k8s API获取applicationName对应的ApplicationConfiguration，过滤筛选其中对应的component
	componentMeta, err := serverlib.RetrieveComponent(ctx, s.KubeClient, applicationName, componentName, namespace)
	if err != nil {
		util.HandleError(c, util.StatusInternalServerError, err)
		return
	}
	util.AssembleResponse(c, componentMeta, nil)
}

// DeleteComponent deletes a component from cluster
func (s *APIServer) DeleteComponent(c *gin.Context) {
	envName := c.Param("envName")
	envMeta, err := env.GetEnvByName(envName)
	if err != nil {
		util.HandleError(c, util.StatusInternalServerError, err)
		return
	}
	appName := c.Param("appName")
	componentName := c.Param("compName")

	o := serverlib.DeleteOptions{
		Client:   s.KubeClient,
		Env:      envMeta,
		AppName:  appName,
		CompName: componentName}
	// 删本地AppFile中Services下的某个service
	// 删k8s中OAM AppConfiguration中的component
	// 删k8s中实际部署的Component
	message, err := o.DeleteComponent(
		cmdutil.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
	util.AssembleResponse(c, message, err)
}
