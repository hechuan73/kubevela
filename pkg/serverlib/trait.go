package serverlib

import (
	"context"
	"fmt"
	"strings"

	"cuelang.org/go/cue"
	plur "github.com/gertd/go-pluralize"
	"github.com/spf13/pflag"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/oam-dev/kubevela/apis/types"
	"github.com/oam-dev/kubevela/pkg/appfile/storage/driver"
	"github.com/oam-dev/kubevela/pkg/application"
	cmdutil "github.com/oam-dev/kubevela/pkg/commands/util"
	"github.com/oam-dev/kubevela/pkg/plugins"
)

// ListTraitDefinitions will list all definition include traits and workloads
func ListTraitDefinitions(workloadName *string) ([]types.Capability, error) {
	var traitList []types.Capability
	traits, err := plugins.LoadInstalledCapabilityWithType(types.TypeTrait)
	if err != nil {
		return traitList, err
	}
	workloads, err := plugins.LoadInstalledCapabilityWithType(types.TypeWorkload)
	if err != nil {
		return traitList, err
	}
	traitList = convertAllAppliyToList(traits, workloads, workloadName)
	return traitList, nil
}

// GetTraitDefinition will get trait capability with applyTo converted
func GetTraitDefinition(workloadName *string, traitType string) (types.Capability, error) {
	var traitDef types.Capability
	// 获取trait
	traitCap, err := plugins.GetInstalledCapabilityWithCapName(types.TypeTrait, traitType)
	if err != nil {
		return traitDef, err
	}
	// 加载所有workload
	workloadsCap, err := plugins.LoadInstalledCapabilityWithType(types.TypeWorkload)
	if err != nil {
		return traitDef, err
	}
	// 通过workloadName提取那些需要apply的trait。如果workloadName为空，则从trait角度，返回所有trait
	// 这里传进去的trait实质上只有一个，其实就是去验证该trait是否会被apply到workload上
	traitList := convertAllAppliyToList([]types.Capability{traitCap}, workloadsCap, workloadName)
	if len(traitList) != 1 {
		return traitDef, fmt.Errorf("could not get installed capability by %s", traitType)
	}
	traitDef = traitList[0]
	return traitDef, nil
}

func convertAllAppliyToList(traits []types.Capability, workloads []types.Capability, workloadName *string) []types.Capability {
	var traitList []types.Capability
	for _, t := range traits {
		// 检查哪些workload是会被这些trait apply的，将workload 名称提取出来
		convertedApplyTo := ConvertApplyTo(t.AppliesTo, workloads)
		if *workloadName != "" {
			if !in(convertedApplyTo, *workloadName) {
				continue
			}
			convertedApplyTo = []string{*workloadName}
		}
		t.AppliesTo = convertedApplyTo
		traitList = append(traitList, t)
	}
	return traitList
}

// ConvertApplyTo will convert applyTo slice to workload capability name if CRD matches
func ConvertApplyTo(applyTo []string, workloads []types.Capability) []string {
	var converted []string
	for _, v := range applyTo {
		newName, exist := check(v, workloads)
		if !exist {
			continue
		}
		if !in(converted, newName) {
			converted = append(converted, newName)
		}
	}
	return converted
}

func check(applyto string, workloads []types.Capability) (string, bool) {
	for _, v := range workloads {
		// 如果trait的apiGroup/Version.Kind与workload的名称或者crdName相同，则可认为该trait是apply到该workload上的
		// 此处workload与component对等
		if Parse(applyto) == v.CrdName || Parse(applyto) == v.Name {
			return v.Name, true
		}
	}
	return "", false
}

func in(l []string, v string) bool {
	for _, ll := range l {
		if ll == v {
			return true
		}
	}
	return false
}

// Parse will parse applyTo(with format apigroup/Version.Kind) to crd name by just calculate the plural of kind word.
// TODO we should use discoverymapper instead of calculate plural
func Parse(applyTo string) string {
	l := strings.Split(applyTo, "/")
	if len(l) != 2 {
		return applyTo
	}
	apigroup, versionKind := l[0], l[1]
	l = strings.Split(versionKind, ".")
	if len(l) != 2 {
		return applyTo
	}
	return plur.NewClient().Plural(strings.ToLower(l[1])) + "." + apigroup
}

// ValidateAndMutateForCore was built in validate and mutate function for core workloads and traits
func ValidateAndMutateForCore(traitType, workloadName string, flags *pflag.FlagSet, env *types.EnvMeta) error {
	switch traitType {
	case "route":
		// 验证domain
		domain, _ := flags.GetString("domain")
		if domain == "" {
			if env.Domain == "" {
				return fmt.Errorf("--domain is required if not contain in environment")
			}
			if strings.HasPrefix(env.Domain, "https://") {
				env.Domain = strings.TrimPrefix(env.Domain, "https://")
			}
			if strings.HasPrefix(env.Domain, "http://") {
				env.Domain = strings.TrimPrefix(env.Domain, "http://")
			}
			if err := flags.Set("domain", workloadName+"."+env.Domain); err != nil {
				return fmt.Errorf("set flag for vela-core trait('route') err %w, please make sure your template is right", err)
			}
		}
		// 验证issuer
		issuer, _ := flags.GetString("issuer")
		if issuer == "" && env.Issuer != "" {
			if err := flags.Set("issuer", env.Issuer); err != nil {
				return fmt.Errorf("set flag for vela-core trait('route') err %w, please make sure your template is right", err)
			}
		}
	default:
		// extend other trait here in the future
	}
	return nil
}

// AddOrUpdateTrait attach trait to workload
func AddOrUpdateTrait(env *types.EnvMeta, appName string, componentName string, flagSet *pflag.FlagSet, template types.Capability) (*driver.Application, error) {
	// 验证trait、workload信息，目前只有route trait
	err := ValidateAndMutateForCore(template.Name, componentName, flagSet, env)
	if err != nil {
		return nil, err
	}
	if appName == "" {
		appName = componentName
	}
	// 获取application -> AppFile
	app, err := application.Load(env.Name, appName)
	if err != nil {
		return app, err
	}
	traitAlias := template.Name
	// 把AppFile中定义的trait数据取出来
	traitData, err := application.GetTraitsByType(app, componentName, traitAlias)
	if err != nil {
		return app, err
	}
	// 把capability中已配置安装的trait参数，apply到AppFile中的去
	for _, v := range template.Parameters {
		name := v.Name
		if v.Alias != "" {
			name = v.Alias
		}
		// nolint:exhaustive
		switch v.Type {
		case cue.IntKind:
			traitData[v.Name], err = flagSet.GetInt64(name)
		case cue.StringKind:
			traitData[v.Name], err = flagSet.GetString(name)
		case cue.BoolKind:
			traitData[v.Name], err = flagSet.GetBool(name)
		case cue.NumberKind, cue.FloatKind:
			traitData[v.Name], err = flagSet.GetFloat64(name)
		default:
			// Currently we don't support get value from complex type
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("get flag(s) \"%s\" err %w", name, err)
		}
	}
	if err = application.SetTrait(app, componentName, traitAlias, traitData); err != nil {
		return app, err
	}
	// 刷新本地的application存储
	return app, application.Save(app, env.Name)
}

// TraitOperationRun will check if it's a stage operation before run
func TraitOperationRun(ctx context.Context, c client.Client, env *types.EnvMeta, appObj *driver.Application,
	staging bool, io cmdutil.IOStreams) (string, error) {
	if staging {
		return "Staging saved", nil
	}
	// 将AppFile构建成OAM Application，并调用k8s API同步更新
	err := application.BuildRun(ctx, appObj, c, env, io)
	if err != nil {
		return "", err
	}
	return "Deployed!", nil
}

// PrepareDetachTrait will detach trait in local AppFile
func PrepareDetachTrait(envName string, traitType string, componentName string, appName string) (*driver.Application, error) {
	var appObj *driver.Application
	var err error
	if appName == "" {
		appName = componentName
	}
	// 通过本地的AppFile，加载出Application
	if appObj, err = application.Load(envName, appName); err != nil {
		return appObj, err
	}
	// 去掉待删除trait信息
	if err = application.RemoveTrait(appObj, componentName, traitType); err != nil {
		return appObj, err
	}
	// 刷新本地存储
	return appObj, application.Save(appObj, envName)
}
