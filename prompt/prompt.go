// Package prompt 提供了基于 text/template 的动态提示词模板管理与严格字段渲染。
package prompt

import (
	"bytes"
	"text/template"
)

// DocAssistantTmpl 是一个预置的问答知识库助手提示词模板样例。
const DocAssistantTmpl = `你是 {{.Product}} 的文档助手。

规则：
- 只依据下方「资料」回答，不编造；资料里没有就明确说"资料未涵盖"。
- 回答简洁、准确，涉及操作时给出清晰步骤。

资料：
{{range .Docs}}- {{.}}
{{end}}
示例（学习这种语气和结构）：
用户：如何修改默认超时？
助手：在配置文件里设置 timeout 字段即可，单位为秒，默认 30。需要我给出完整示例吗？`

// Template 封装了 Go 标准库 text/template，并强制开启 missingkey=error 防止模板变量遗漏。
type Template struct {
	tmpl *template.Template
}

// New 解析提示词文本并创建 Template 实例。
func New(name, text string) (*Template, error) {
	t, err := template.New(name).Option("missingkey=error").Parse(text)
	if err != nil {
		return nil, err
	}
	return &Template{tmpl: t}, nil
}

// Render 将传入的数据结构渲染进模板生成最终的提示词文本。
func (t *Template) Render(data any) (string, error) {
	var buf bytes.Buffer
	if err := t.tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
