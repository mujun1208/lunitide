package projectgen

func Skeleton(documentType string) (string, bool) {
	body, ok := skeletons[documentType]
	return body, ok
}

func PhaseDocumentTypes(phase int) []string {
	if phase == 1 {
		return []string{"biz_req_analysis", "impl_assessment", "req_task_list", "arch_design", "hw_config", "biz_standard", "dev_standard", "tech_standard", "project_structure"}
	}
	if phase == 2 {
		return []string{"biz_flow_diagram", "biz_flow_list", "biz_blueprint", "api_list", "feature_dev_list", "feature_detail", "api_detail", "db_detail", "ui_detail", "integration_test_list"}
	}
	return nil
}

var skeletons = map[string]string{
	"biz_req_analysis":      mdSkeleton("业务需求分析报告", "说明要解决的业务问题、用户、范围与非目标。"),
	"impl_assessment":       mdSkeleton("系统实现评估报告", "评估实现路径、风险、依赖与工作量。"),
	"req_task_list":         checklistSkeleton("待细化：{{answer.core_problem}}"),
	"arch_design":           mdSkeleton("系统架构设计文档", "给出分层、边界、关键组件与数据流。"),
	"hw_config":             mdSkeleton("系统硬件配置文档", "给出本机/服务器最低与推荐配置。"),
	"biz_standard":          mdSkeleton("系统业务规范", "约定业务名词、状态、权限与例外处理。"),
	"dev_standard":          mdSkeleton("系统开发规范", "约定目录、命名、测试、提交与禁止事项。后续开发必须遵守。"),
	"tech_standard":         mdSkeleton("系统技术规范", "约定语言、框架、存储、日志、错误码与安全底线。后续开发必须遵守。"),
	"project_structure":     mdSkeleton("项目结构规范", "说明目录职责。机器树在确认后生成，可先审默认树。"),
	"biz_flow_diagram":      mdSkeleton("业务流程图", "用文字+ mermaid 描述主路径与例外路径。"),
	"biz_flow_list":         checklistSkeleton("主业务流：{{answer.main_flows}}"),
	"biz_blueprint":         mdSkeleton("业务蓝图文档", "把阶段 1 需求展开为可设计的业务蓝图。"),
	"api_list":              checklistSkeleton("待设计接口：{{answer.api_style}}"),
	"feature_dev_list":      checklistSkeleton("待开发功能：{{answer.module_cut}}"),
	"feature_detail":        mdSkeleton("功能详细设计文档", "按功能清单逐条写入口、规则、验收。"),
	"api_detail":            mdSkeleton("接口详细设计文档", "按接口清单写方法、路径、入参、出参、错误。"),
	"db_detail":             mdSkeleton("数据库详细设计文档", "给出表、列、主键。机器模式可另存 ```json DatabaseSchemaV1。"),
	"ui_detail":             mdSkeleton("UI界面详细设计", "按 {{answer.ui_depth}} 描述页面、状态与空错态。"),
	"integration_test_list": checklistSkeleton("集成场景：{{answer.integration_cut}}"),
	"db_design":             mdSkeleton("数据库设计文档", "确认将物化的表结构，并附机器模式。"),
	"interface_list":        checklistSkeleton("接口工作台条目"),
	"dev_checklist":         checklistSkeleton("开发任务"),
	"test_checklist":        checklistSkeleton("测试任务"),
}

func mdSkeleton(title, purpose string) string {
	return "# {{title}}\n\n项目：{{projectName}}（{{projectCode}}）  \n类型：{{projectType}}  \n日期：{{date}}  \n阶段：{{phaseLabel}}\n\n## 目的\n\n" + purpose + "\n\n## 范围\n\n- 核心问题：{{answer.core_problem}}\n- 形态：{{answer.system_shape}}\n- 技术栈：{{answer.stack}}\n\n## 正文\n\n请在本骨架上补全项目专属内容。前序摘要：\n\n{{prior}}\n\n专家综合：\n\n{{council}}\n\n## 修订记录\n\n| 日期 | 说明 |\n|---|---|\n| {{date}} | 按模版或完整格式首稿 |\n"
}

func checklistSkeleton(title string) string {
	return "{\n  \"version\": 1,\n  \"items\": [\n    {\n      \"id\": \"F001\",\n      \"title\": \"" + title + "\",\n      \"status\": \"pending\"\n    }\n  ]\n}\n"
}
