package server

import (
	"github.com/oam-dev/kubevela/pkg/plugins"
	"github.com/oam-dev/kubevela/pkg/server/util"
	"github.com/oam-dev/kubevela/pkg/serverlib"

	"github.com/gin-gonic/gin"
)

// AddCapabilityCenter adds and synchronizes a capability center from remote
func (s *APIServer) AddCapabilityCenter(c *gin.Context) {
	var body plugins.CapCenterConfig
	if err := c.ShouldBindJSON(&body); err != nil {
		util.HandleError(c, util.StatusInternalServerError, "the add capability center request body is invalid")
		return
	}
	// 添加repoConfig到 /USER_HOME/.vela/centers/config.yaml中，并同步更新每一个repo，将CRDDefinition存储到
	// /USER_HOME/.vela/centers/centerName/下
	if err := serverlib.AddCapabilityCenter(body.Name, body.Address, body.Token); err != nil {
		util.HandleError(c, util.StatusInternalServerError, err.Error())
		return
	}
	util.AssembleResponse(c, "Successfully configured capability center and synchronized from remote", nil)
}

// ListCapabilityCenters list all added capability centers
func (s *APIServer) ListCapabilityCenters(c *gin.Context) {
	// 解析/USER_HOME/.vela/centers/config.yaml，返回centers
	capabilityCenterList, err := serverlib.ListCapabilityCenters()
	if err != nil {
		util.HandleError(c, util.StatusInternalServerError, err.Error())
		return
	}
	util.AssembleResponse(c, capabilityCenterList, nil)
}

// SyncCapabilityCenter synchronizes capability center from remote
func (s *APIServer) SyncCapabilityCenter(c *gin.Context) {
	capabilityCenterName := c.Param("capabilityCenterName")
	if err := serverlib.SyncCapabilityCenter(capabilityCenterName); err != nil {
		util.HandleError(c, util.StatusInternalServerError, err.Error())
		return
	}
	util.AssembleResponse(c, "sync finished", nil)
}

// AddCapabilityIntoCluster adds specific capability into cluster
func (s *APIServer) AddCapabilityIntoCluster(c *gin.Context) {
	cap := c.Param("capabilityCenterName") + "/" + c.Param("capabilityName")
	msg, err := serverlib.AddCapabilityIntoCluster(s.KubeClient, s.dm, cap)
	if err != nil {
		util.HandleError(c, util.StatusInternalServerError)
		return
	}
	util.AssembleResponse(c, msg, nil)
}

// DeleteCapabilityCenter deltes a capability cernter already added
func (s *APIServer) DeleteCapabilityCenter(c *gin.Context) {
	capabilityCenterName := c.Param("capabilityCenterName")
	msg, err := serverlib.RemoveCapabilityCenter(capabilityCenterName)
	util.AssembleResponse(c, msg, err)
}

// RemoveCapabilityFromCluster remove a specific capability from cluster
func (s *APIServer) RemoveCapabilityFromCluster(c *gin.Context) {
	capabilityCenterName := c.Param("capabilityName")
	msg, err := serverlib.RemoveCapabilityFromCluster(s.KubeClient, capabilityCenterName)
	if err != nil {
		util.HandleError(c, util.StatusInternalServerError, err.Error())
		return
	}
	util.AssembleResponse(c, msg, nil)
}

// ListCapabilities lists capabilities of a capability center
func (s *APIServer) ListCapabilities(c *gin.Context) {
	capabilityCenterName := c.Param("capabilityName")
	capabilityList, err := serverlib.ListCapabilities(capabilityCenterName)
	if err != nil {
		util.HandleError(c, util.StatusInternalServerError, err.Error())
		return
	}
	util.AssembleResponse(c, capabilityList, nil)
}
