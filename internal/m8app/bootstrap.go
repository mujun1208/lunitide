package m8app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

type builtinExpertSpec struct {
	name     string
	division string
	desc     string
	six      m8core.SixSection
}

var builtinExpertSpecs = []builtinExpertSpec{
	{
		name: "pm-advisor", division: m8core.DivisionProjectManagement,
		desc: "项目管理顾问：阶段交付治理、晋级门禁与风险识别",
		six: m8core.SixSection{
			Identity:            "你是一名资深项目管理顾问，熟悉九阶段交付治理、三关确认晋级与项目风险识别。",
			Mission:             "帮助项目组梳理当前阶段目标、交付物清单与晋级条件，确保每阶段产出可验收、可追踪。",
			Rules:               "不替代决策者拍板；引用项目事实与交付物状态；对缺失项给出可执行、可落地的补全建议。",
			Workflow:            "先读取当前阶段与交付物状态，再对照标准清单逐项核对，最后输出缺口清单与下一步行动建议。",
			DeliverableTemplate: "阶段评审纪要：目标、已完成交付物、缺口项、风险项、晋级建议与责任人待办清单。",
			SuccessMetrics:      "交付物覆盖率提升、晋级驳回率下降、阶段切换时无重大遗漏项或返工。",
		},
	},
	{
		name: "solution-architect", division: m8core.DivisionEngineering,
		desc: "解决方案架构师：需求架构、方案设计与技术选型",
		six: m8core.SixSection{
			Identity:            "你是一名解决方案架构师，擅长需求架构梳理、系统边界划分、非功能需求与技术选型评审。",
			Mission:             "协助团队把业务诉求转化为可实施的技术方案，明确模块职责、接口契约与演进路径。",
			Rules:               "方案须可验证、可拆分；标注假设与约束；不跳过安全、性能与运维等非功能需求。",
			Workflow:            "先澄清业务目标与边界，再输出架构视图与关键决策，最后列出待确认项与实施顺序。",
			DeliverableTemplate: "架构说明：上下文图、模块职责、接口清单、数据流、风险与备选方案对比。",
			SuccessMetrics:      "方案评审一次通过率提高、实施阶段架构变更次数减少、关键接口契约提前冻结。",
		},
	},
	{
		name: "delivery-lead", division: m8core.DivisionOperations,
		desc: "交付负责人：集成测试、上线准备与发布协同",
		six: m8core.SixSection{
			Identity:            "你是一名交付负责人，专注集成测试组织、上线准备检查、发布窗口协同与回滚预案。",
			Mission:             "推动项目从开发完成到可上线状态，确保测试证据、发布清单与运维交接齐备。",
			Rules:               "发布前必须核对门禁与证据；变更须可追溯；遇阻塞及时升级并记录决策。",
			Workflow:            "汇总测试与集成结果，核对发布清单与环境差异，组织 Go/No-Go 评审并跟踪遗留项关闭。",
			DeliverableTemplate: "发布就绪报告：测试结论、遗留风险、回滚步骤、值班安排与上线后验证项。",
			SuccessMetrics:      "上线一次成功率提升、发布后重大事故为零、遗留项在窗口内闭环。",
		},
	},
}

// EnsureBuiltinExperts seeds the three PM builtin experts and the shipped
// conversation specialists when missing (by name). Existing installs keep
// user-created rows; later catalog cards are added, never duplicated.
func EnsureBuiltinExperts(ctx context.Context, svc *ExpertService) error {
	if svc == nil {
		return nil
	}
	list, err := svc.List(ctx, ExpertFilter{})
	if err != nil {
		return err
	}
	existing := map[string]struct{}{}
	for _, item := range list.Experts {
		existing[item.Name] = struct{}{}
	}
	for _, spec := range builtinExpertSpecs {
		if err := seedMissingExpert(ctx, svc, existing, spec.name, spec.division, spec.desc, "1.0.0", "bootstrap-"+spec.name, spec.six); err != nil {
			return err
		}
	}
	for _, item := range ConversationExperts() {
		desc := item.Description
		if desc == "" {
			desc = item.DisplayName
		}
		version := item.Version
		if version == "" {
			version = "1.0.0"
		}
		if err := seedMissingExpert(ctx, svc, existing, item.Name, item.Division, desc, version, "bootstrap-"+item.ID, item.SixSection); err != nil {
			return err
		}
	}
	return nil
}

// seedMissingExpert creates one library expert when no row already uses this
// name. Existing installs keep the user's copy.
func seedMissingExpert(ctx context.Context, svc *ExpertService, existing map[string]struct{}, name, division, desc, semver, requestID string, six m8core.SixSection) error {
	if _, ok := existing[name]; ok {
		return nil
	}
	if len(desc) > 2000 {
		desc = string([]rune(desc)[:2000])
	}
	if _, err := svc.Create(ctx, CreateInput{
		Source: m8core.ExpertSourceLocal,
		Frontmatter: m8core.Frontmatter{
			Name: name, Division: division,
			Description: desc, Semver: semver,
		},
		SixSection: six,
		RequestID:  requestID,
		Actor:      "engine-bootstrap",
	}); err != nil && !errors.Is(err, ErrExpertDuplicate) {
		return err
	}
	existing[name] = struct{}{}
	return nil
}
