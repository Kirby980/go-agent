package skill

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kirby980/agent/tool"
	"gopkg.in/yaml.v3"
)

var errNoFrontmatter = errors.New("缺少 frontmatter")

type Meta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type Skill struct {
	Meta
	Dir string
}

// expandPath 展开波浪号 ~
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

func Load(root string) ([]Skill, error) {
	root = expandPath(root)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err // 目录不存在直接忽略
	}
	var skills []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		skillFile := filepath.Join(dir, "SKILL.md")

		// 如果不是技能目录（没有 SKILL.md），优雅跳过，绝不能 log.Fatalln
		if _, err := os.Stat(skillFile); os.IsNotExist(err) {
			continue
		}

		meta, err := parseFrontmatter(skillFile)
		if err != nil {
			// 仅记录警告，不退出进程
			continue
		}
		skills = append(skills, Skill{Meta: meta, Dir: dir})
	}
	return skills, nil
}

func parseFrontmatter(path string) (Meta, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Meta{}, err
	}
	s := string(raw)
	if !strings.HasPrefix(s, "---") {
		return Meta{}, err
	}
	end := strings.Index(s[3:], "---")
	if end < 0 {
		return Meta{}, errNoFrontmatter
	}
	var m Meta
	if err := yaml.Unmarshal([]byte(s[3:3+end]), &m); err != nil {
		return Meta{}, err
	}
	return m, nil
}

// Body 在技能被触发时才调用，把正文(L2)读进来。
func (s Skill) Body() (string, error) {
	raw, err := os.ReadFile(filepath.Join(s.Dir, "SKILL.md"))
	if err != nil {
		return "", err
	}
	str := string(raw)
	if strings.HasPrefix(str, "---") {
		if i := strings.Index(str[3:], "---"); i >= 0 {
			return strings.TrimSpace(str[3+i+3:]), nil
		}
	}
	return str, nil
}

type readSkillArgs struct {
	Name string `json:"name" desc:"要查询的技能名称"`
}

func NewSkillTool(Skills []Skill) tool.Tool {
	skillMap := make(map[string]Skill)
	for _, s := range Skills {
		skillMap[strings.ToLower(s.Name)] = s
	}
	return tool.NewTypedTool("read_skill", "按技能名称读取该技能的详细工作流指导、SOP和操作规范。当需要解决特定领域专业问题时先调用此工具。",
		func(ctx context.Context, args readSkillArgs) (string, error) {
			s, ok := skillMap[strings.ToLower(strings.TrimSpace(args.Name))]
			if !ok {
				var available []string
				for name := range skillMap {
					available = append(available, name)
				}
				return "", fmt.Errorf("未找到技能 %q，当前可用技能列表: %s", args.Name, strings.Join(available, ", "))
			}
			body, err := s.Body()
			if err != nil {
				return "", fmt.Errorf("读取技能 %q 失败: %w", args.Name, err)
			}
			return fmt.Sprintf("=== 技能 %s 操作指导 ===\n%s", s.Name, body), nil
		})
}

// FormatSkillsPrompt 生成注入到 System Prompt 的 L1 技能概要列表
func FormatSkillsPrompt(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## 可用专业技能 (Skills)\n")
	sb.WriteString("当你面对以下特定领域的任务时，请先调用 `read_skill` 工具读取对应技能的操作规范与步骤，再开始执行：\n")
	for _, s := range skills {
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", s.Name, s.Description))
	}
	return sb.String()
}
