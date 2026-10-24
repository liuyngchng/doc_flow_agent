// Package interfaces 对外接口注册表
//
// 每个接口在代码中硬编码，通过 interface_path 关联页面创建的提示词模板。
// 不同接口的额外逻辑通过 PreProcess / PostProcess 钩子注入。
package interfaces

import "doc_flow_agent/internal/pipeline"

// Registry 对外接口注册表
// key 为接口路径（如 "document/classify"），value 为接口定义
var Registry = map[string]*pipeline.InterfaceDefinition{
	// IF-02: 收文分类与信息提取
	"document/classify": {
		Path:        "document/classify",
		Name:        "收文分类",
		Description: "判别收文类型（公司收文/部门收文/手动收文）并提取表单信息",
	},

	// IF-03: 流转目标识别
	"document/route": {
		Path:        "document/route",
		Name:        "流转目标识别",
		Description: "根据审批意见识别应选中的下一目标人员或机构",
	},

	// IF-06: 发文审核
	"document/dispatch-review": {
		Path:        "document/dispatch-review",
		Name:        "发文审核",
		Description: "发文流转过程中对公文进行四维合规审核",
	},
}

// List 返回所有接口定义的扁平列表（用于管理后台展示）
func List() []*pipeline.InterfaceDefinition {
	defs := make([]*pipeline.InterfaceDefinition, 0, len(Registry))
	for _, def := range Registry {
		defs = append(defs, def)
	}
	return defs
}