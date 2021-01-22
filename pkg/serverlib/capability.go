package serverlib

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/oam-dev/kubevela/apis/core.oam.dev/v1alpha2"

	"github.com/oam-dev/kubevela/pkg/oam/discoverymapper"
	"github.com/oam-dev/kubevela/pkg/oam/util"

	"github.com/oam-dev/kubevela/apis/types"
	cmdutil "github.com/oam-dev/kubevela/pkg/commands/util"
	"github.com/oam-dev/kubevela/pkg/plugins"
	"github.com/oam-dev/kubevela/pkg/server/apis"
	"github.com/oam-dev/kubevela/pkg/utils/helm"
	"github.com/oam-dev/kubevela/pkg/utils/system"
)

// AddCapabilityCenter will add a cap center
func AddCapabilityCenter(capName, capURL, capToken string) error {
	// 加载 /USER_HOME/.vela/centers/config.yaml中的内容
	repos, err := plugins.LoadRepos()
	if err != nil {
		return err
	}
	config := &plugins.CapCenterConfig{
		Name:    capName,
		Address: capURL,
		Token:   capToken,
	}
	var updated bool
	for idx, r := range repos {
		if r.Name == config.Name {
			repos[idx] = *config
			updated = true
			break
		}
	}
	if !updated {
		repos = append(repos, *config)
	}
	// 刷新本地存储
	if err = plugins.StoreRepos(repos); err != nil {
		return err
	}
	// 拉取GitHub信息，刷新本地的capabilities
	return SyncCapabilityFromCenter(capName, capURL, capToken)
}

// SyncCapabilityFromCenter will sync all capabilities from center
func SyncCapabilityFromCenter(capName, capURL, capToken string) error {
	// 通过capURL创建client，目前支持的是GitHub
	client, err := plugins.NewCenterClient(context.Background(), capName, capURL, capToken)
	if err != nil {
		return err
	}
	// 拉取仓库中的CRDDefinition（WorkloadDefinition、TraitDefinition、ScopeDefinition）
	// 存储到本地 /USER_HOME/.vela/centers/centerName/XXXDefinition.yaml
	return client.SyncCapabilityFromCenter()
}

// AddCapabilityIntoCluster will add a capability into K8s cluster, it is equal to apply a definition yaml and run `vela workloads/traits`
func AddCapabilityIntoCluster(c client.Client, mapper discoverymapper.DiscoveryMapper, capability string) (string, error) {
	ss := strings.Split(capability, "/")
	if len(ss) < 2 {
		return "", errors.New("invalid format for " + capability + ", please follow format <center>/<name>")
	}
	repoName := ss[0]
	name := ss[1]
	ioStreams := cmdutil.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	// 安装Capability，分为以下几步
	// 1、读取/USER_HOME/.vela/centers/repoName/capabilityName文件，转化为XXXDefinition，构建Capability
	// 2、根据Capability的安装方式，使用helm等方式安装Capability
	// 3、将Capability对应的CRD同步到k8s中，将Capability刷新到本地存储/capabilities下
	if err := InstallCapability(c, mapper, repoName, name, ioStreams); err != nil {
		return "", err
	}
	return fmt.Sprintf("Successfully installed capability %s from %s", name, repoName), nil
}

// InstallCapability will add a cap into K8s cluster and install it's controller(helm charts)
func InstallCapability(client client.Client, mapper discoverymapper.DiscoveryMapper, centerName, capabilityName string, ioStreams cmdutil.IOStreams) error {
	// /USER_HOME/.vela/centers
	dir, _ := system.GetCapCenterDir()
	repoDir := filepath.Join(dir, centerName)
	// 通过/USER_HOME/.vela/centers/repoName/capabilityName文件，转化为XXXDefinition，构建Capability
	tp, err := GetCapabilityFromCenter(centerName, capabilityName)
	if err != nil {
		return err
	}
	tp.Source = &types.Source{RepoName: centerName}
	// /USER_HOME/.vela/capabilities
	defDir, _ := system.GetCapabilityDir()
	switch tp.Type {
	case types.TypeWorkload:
		var wd v1alpha2.WorkloadDefinition
		// 读取/USER_HOME/.vela/centers/repoName/crdName.yaml
		workloadData, err := ioutil.ReadFile(filepath.Clean(filepath.Join(repoDir, tp.CrdName+".yaml")))
		if err != nil {
			return nil
		}
		if err = yaml.Unmarshal(workloadData, &wd); err != nil {
			return err
		}
		wd.Namespace = types.DefaultKubeVelaNS
		ioStreams.Info("Installing workload capability " + wd.Name)
		if tp.Install != nil {
			tp.Source.ChartName = tp.Install.Helm.Name
			// 通过helm安装Capability
			if err = helm.InstallHelmChart(ioStreams, tp.Install.Helm); err != nil {
				return err
			}
		}
		// 解析GroupVersionKind
		gvk, err := util.GetGVKFromDefinition(mapper, wd.Spec.Reference)
		if err != nil {
			return err
		}
		tp.CrdInfo = &types.CRDInfo{
			APIVersion: gvk.GroupVersion().String(),
			Kind:       gvk.Kind,
		}
		// 将WorkloadDefinition更新存储到k8s中
		if err = client.Create(context.Background(), &wd); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	case types.TypeTrait:
		// Trait和Workload的步骤类似
		var td v1alpha2.TraitDefinition
		traitdata, err := ioutil.ReadFile(filepath.Clean(filepath.Join(repoDir, tp.CrdName+".yaml")))
		if err != nil {
			return nil
		}
		if err = yaml.Unmarshal(traitdata, &td); err != nil {
			return err
		}
		td.Namespace = types.DefaultKubeVelaNS
		ioStreams.Info("Installing trait capability " + td.Name)
		if tp.Install != nil {
			tp.Source.ChartName = tp.Install.Helm.Name
			if err = helm.InstallHelmChart(ioStreams, tp.Install.Helm); err != nil {
				return err
			}
		}
		gvk, err := util.GetGVKFromDefinition(mapper, td.Spec.Reference)
		if err != nil {
			return err
		}
		tp.CrdInfo = &types.CRDInfo{
			APIVersion: gvk.GroupVersion().String(),
			Kind:       gvk.Kind,
		}
		if err = client.Create(context.Background(), &td); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	case types.TypeScope:
		// TODO(wonderflow): support install scope here
	}
	// 将安装的Capability刷新到本地/USER_HOME/.vela/capabilities下
	success := plugins.SinkTemp2Local([]types.Capability{tp}, defDir)
	if success == 1 {
		ioStreams.Infof("Successfully installed capability %s from %s\n", capabilityName, centerName)
	}
	return nil
}

// GetCapabilityFromCenter will list all synced capabilities from cap center and return the specified one
func GetCapabilityFromCenter(repoName, addonName string) (types.Capability, error) {
	dir, _ := system.GetCapCenterDir()
	// /USER_HOME/.vela/centers/repoName
	repoDir := filepath.Join(dir, repoName)
	// 读取所有的/USER_HOME/.vela/centers/repoName/capabilityName文件，转化为XXXDefinition，然后拉取CUE文件，一起转化成Capability。
	// 其中CUE文件存在.tmp下，Capability本身没有存储
	templates, err := plugins.LoadCapabilityFromSyncedCenter(repoDir)
	if err != nil {
		return types.Capability{}, err
	}
	for _, t := range templates {
		if t.Name == addonName {
			return t, nil
		}
	}
	return types.Capability{}, fmt.Errorf("%s/%s not exist, try vela cap:center:sync %s to sync from remote", repoName, addonName, repoName)
}

// ListCapabilityCenters will list all capabilities from center
func ListCapabilityCenters() ([]apis.CapabilityCenterMeta, error) {
	var capabilityCenterList []apis.CapabilityCenterMeta
	centers, err := plugins.LoadRepos()
	if err != nil {
		return capabilityCenterList, err
	}
	for _, c := range centers {
		capabilityCenterList = append(capabilityCenterList, apis.CapabilityCenterMeta{
			Name: c.Name,
			URL:  c.Address,
		})
	}
	return capabilityCenterList, nil
}

// SyncCapabilityCenter will sync capabilities from center to local
func SyncCapabilityCenter(capabilityCenterName string) error {
	repos, err := plugins.LoadRepos()
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		return fmt.Errorf("no capability center configured")
	}
	find := false
	if capabilityCenterName != "" {
		for idx, r := range repos {
			if r.Name == capabilityCenterName {
				repos = []plugins.CapCenterConfig{repos[idx]}
				find = true
				break
			}
		}
		if !find {
			return fmt.Errorf("%s center not exist", capabilityCenterName)
		}
	}
	ctx := context.Background()
	for _, d := range repos {
		client, err := plugins.NewCenterClient(ctx, d.Name, d.Address, d.Token)
		if err != nil {
			return err
		}
		// 拉取GitHub信息，刷新本地的/centers/centerName/下的capabilityDefinition，CUE存在centers/.tmp下
		err = client.SyncCapabilityFromCenter()
		if err != nil {
			return err
		}
	}
	return nil
}

// RemoveCapabilityFromCluster will remove a capability from cluster.
// 1. remove definition 2. uninstall chart 3. remove local files
func RemoveCapabilityFromCluster(client client.Client, capabilityName string) (string, error) {
	ioStreams := cmdutil.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	if err := RemoveCapability(client, capabilityName, ioStreams); err != nil {
		return "", err
	}
	msg := fmt.Sprintf("%s removed successfully", capabilityName)
	return msg, nil
}

// RemoveCapability will remove a capability from cluster.
// 1. remove definition 2. uninstall chart 3. remove local files
func RemoveCapability(client client.Client, capabilityName string, ioStreams cmdutil.IOStreams) error {
	// TODO(wonderflow): make sure no apps is using this capability
	caps, err := plugins.LoadAllInstalledCapability()
	if err != nil {
		return err
	}
	for _, w := range caps {
		if w.Name == capabilityName {
			return uninstallCap(client, w, ioStreams)
		}
	}
	return errors.New(capabilityName + " not exist")
}

func uninstallCap(client client.Client, cap types.Capability, ioStreams cmdutil.IOStreams) error {
	// 1. Remove WorkloadDefinition or TraitDefinition
	ctx := context.Background()
	var obj runtime.Object
	switch cap.Type {
	case types.TypeTrait:
		obj = &v1alpha2.TraitDefinition{ObjectMeta: v1.ObjectMeta{Name: cap.Name, Namespace: types.DefaultKubeVelaNS}}
	case types.TypeWorkload:
		obj = &v1alpha2.WorkloadDefinition{ObjectMeta: v1.ObjectMeta{Name: cap.Name, Namespace: types.DefaultKubeVelaNS}}
	case types.TypeScope:
		return fmt.Errorf("uninstall scope capability was not supported yet")
	}
	if err := client.Delete(ctx, obj); err != nil {
		return err
	}

	if cap.Install != nil && cap.Install.Helm.Name != "" {
		// 2. Remove Helm chart if there is
		if cap.Install.Helm.Namespace == "" {
			cap.Install.Helm.Namespace = types.DefaultKubeVelaNS
		}
		if err := helm.Uninstall(ioStreams, cap.Install.Helm.Name, cap.Install.Helm.Namespace, cap.Name); err != nil {
			return err
		}
	}

	// 3. Remove local capability file
	capdir, _ := system.GetCapabilityDir()
	switch cap.Type {
	case types.TypeTrait:
		return os.Remove(filepath.Join(capdir, "traits", cap.Name))
	case types.TypeWorkload:
		return os.Remove(filepath.Join(capdir, "workloads", cap.Name))
	case types.TypeScope:
		// TODO(wonderflow): add scope remove here.
	}
	ioStreams.Infof("%s removed successfully", cap.Name)
	return nil
}

// ListCapabilities will list all caps from specified center
func ListCapabilities(capabilityCenterName string) ([]types.Capability, error) {
	var capabilityList []types.Capability
	dir, err := system.GetCapCenterDir()
	if err != nil {
		return capabilityList, err
	}
	if capabilityCenterName != "" {
		return listCenterCapabilities(filepath.Join(dir, capabilityCenterName))
	}
	dirs, err := ioutil.ReadDir(dir)
	if err != nil {
		return capabilityList, err
	}
	for _, dd := range dirs {
		if !dd.IsDir() {
			continue
		}
		caps, err := listCenterCapabilities(filepath.Join(dir, dd.Name()))
		if err != nil {
			return capabilityList, err
		}
		capabilityList = append(capabilityList, caps...)
	}
	return capabilityList, nil
}

func listCenterCapabilities(repoDir string) ([]types.Capability, error) {
	templates, err := plugins.LoadCapabilityFromSyncedCenter(repoDir)
	if err != nil {
		return templates, err
	}
	if len(templates) < 1 {
		return templates, nil
	}
	baseDir := filepath.Base(repoDir)
	workloads := gatherWorkloads(templates)
	for i, p := range templates {
		status := checkInstallStatus(baseDir, p)
		convertedApplyTo := ConvertApplyTo(p.AppliesTo, workloads)
		templates[i].Center = baseDir
		templates[i].Status = status
		templates[i].AppliesTo = convertedApplyTo
	}
	return templates, nil
}

// 这里只是删除了CapabilityCenter下的东西，对于/capabilities下的东西，以及k8s集群里的东西都没删
// RemoveCapabilityCenter will remove a cap center from local
func RemoveCapabilityCenter(centerName string) (string, error) {
	var message string
	var err error
	dir, _ := system.GetCapCenterDir()
	repoDir := filepath.Join(dir, centerName)
	// 1.remove capability center dir
	if _, err := os.Stat(repoDir); err != nil {
		if os.IsNotExist(err) {
			err = fmt.Errorf("%s capability center has not successfully synced", centerName)
			return message, err
		}
	}
	// 删除/USER_HOME/.vela/centers/centerName下的所有东西
	if err = os.RemoveAll(repoDir); err != nil {
		return message, err
	}
	// 2.remove center from capability center config
	// 刷新/USER_HOME/.vela/centers/config.yaml文件
	repos, err := plugins.LoadRepos()
	if err != nil {
		return message, err
	}
	for idx, r := range repos {
		if r.Name == centerName {
			repos = append(repos[:idx], repos[idx+1:]...)
			break
		}
	}
	if err = plugins.StoreRepos(repos); err != nil {
		return message, err
	}
	message = fmt.Sprintf("%s capability center removed successfully", centerName)
	return message, err
}

func gatherWorkloads(templates []types.Capability) []types.Capability {
	workloads, err := plugins.LoadInstalledCapabilityWithType(types.TypeWorkload)
	if err != nil {
		workloads = make([]types.Capability, 0)
	}
	for _, t := range templates {
		if t.Type == types.TypeWorkload {
			workloads = append(workloads, t)
		}
	}
	return workloads
}

func checkInstallStatus(repoName string, tmp types.Capability) string {
	var status = "uninstalled"
	installed, _ := plugins.LoadInstalledCapabilityWithType(tmp.Type)
	for _, i := range installed {
		if i.Source != nil && i.Source.RepoName == repoName && i.Name == tmp.Name && i.CrdName == tmp.CrdName {
			return "installed"
		}
	}
	return status
}
