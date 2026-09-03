package plan

import (
	"fmt"
	"sort"
	"strings"
)

// Levels 把任务按拓扑层级分组：同一层内的任务无相互依赖，可并行执行。
// 若存在循环依赖或依赖了不存在的任务 ID，返回错误。
func Levels(plan Plan) ([][]string, error) {
	// 第 1 步：建立任务 ID 集合，用于校验 DependsOn 指向的是合法任务。
	exists := make(map[string]bool, len(plan.Tasks))
	for _, task := range plan.Tasks {
		task.ID = strings.TrimSpace(task.ID)
		if task.ID == "" {
			return nil, fmt.Errorf("任务 ID 不能为空")
		}
		if exists[task.ID] {
			return nil, fmt.Errorf("任务 ID %q 重复", task.ID)
		}
		exists[task.ID] = true
	}

	indeg := make(map[string]int, len(plan.Tasks)) // 每个任务剩余的未满足依赖数
	dependents := make(map[string][]string)        // dep -> 依赖它的任务列表
	for _, task := range plan.Tasks {
		if _, ok := indeg[task.ID]; !ok {
			indeg[task.ID] = 0
		}
		for _, dep := range task.DependsOn {
			dep = strings.TrimSpace(dep)
			// 第 2 步：校验依赖存在，避免后面被错认为"循环依赖"。
			if !exists[dep] {
				return nil, fmt.Errorf("任务 %q 依赖了不存在的任务 %q", task.ID, dep)
			}
			indeg[task.ID]++
			dependents[dep] = append(dependents[dep], task.ID)
		}
	}

	// 首层：入度为 0 的任务
	var current []string
	for id, degree := range indeg {
		if degree == 0 {
			current = append(current, id)
		}
	}

	var levels [][]string
	done := 0
	for len(current) > 0 {
		sort.Strings(current) // 排序只为输出稳定、便于测试
		level := append([]string(nil), current...)
		levels = append(levels, level)
		done += len(level)

		var next []string
		for _, id := range level {
			for _, dependent := range dependents[id] {
				indeg[dependent]--
				if indeg[dependent] == 0 {
					next = append(next, dependent)
				}
			}
		}
		current = next
	}

	// 走到这里 done 仍 < len(indeg) 的，只可能是循环依赖
	// （非法依赖已经在前面被拦下）
	if done != len(indeg) {
		return nil, fmt.Errorf("计划存在循环依赖，无法执行")
	}
	return levels, nil
}
