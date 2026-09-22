package plan

import (
	"fmt"
	"sort"
	"strings"
)

// Levels 把 DAG 任务图按照拓扑分层（Topological Levels）算法进行分组。
//
// 核心逻辑与算法设计：
// 1. 完整性校验：
//    - 校验所有任务 ID 不能为空且不能重复。
//    - 校验 DependsOn 引用的所有依赖任务在图中真实存在，避免空指针与误报。
// 2. 入度统计（Indegree）与反向依赖图构建（Dependents Graph）：
//    - 统计每个任务当前未就绪的前置依赖数量（入度 indeg）。
//    - 建立反向图映射 dependents[depID] -> 依赖该前置任务的后置任务列表。
// 3. Kahn 拓扑分层循环（逐层剥离入度为 0 的节点）：
//    - 第 0 层：所有入度为 0 的任务（无任何前置依赖，可立即并行启动）。
//    - 第 N 层：当第 N-1 层所有任务“虚拟执行完毕”后，将其所有后置任务的入度减 1。
//      若某个后置任务入度归零，说明其所有前置依赖已在本层或更早层满足，加入下一层。
// 4. 循环依赖检测（Cycle Detection）：
//    - 若遍历结束时已分层的任务总数 < 全量任务总数，说明图中存在闭环（如 A->B->A），返回错误。
//
// 返回值：
// 二维切片 [][]string，其中 levels[0] 是首批并行任务，levels[1] 依赖 levels[0]，以此类推。
func Levels(plan Plan) ([][]string, error) {
	// 第 1 步：建立任务 ID 集合，校验任务合法性与唯一性
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

	// 第 2 步：统计每个任务节点的入度（未就绪的依赖数）以及反向依赖映射表
	indeg := make(map[string]int, len(plan.Tasks)) // 任务 ID -> 剩余依赖数量
	dependents := make(map[string][]string)        // 前置依赖 ID -> 依赖它的后续任务列表
	for _, task := range plan.Tasks {
		if _, ok := indeg[task.ID]; !ok {
			indeg[task.ID] = 0
		}
		for _, dep := range task.DependsOn {
			dep = strings.TrimSpace(dep)
			// 校验前置依赖是否存在，杜绝悬空依赖
			if !exists[dep] {
				return nil, fmt.Errorf("任务 %q 依赖了不存在的任务 %q", task.ID, dep)
			}
			indeg[task.ID]++
			dependents[dep] = append(dependents[dep], task.ID)
		}
	}

	// 第 3 步：提取首层任务 —— 入度为 0 的节点（即无需等待任何前置依赖）
	var current []string
	for id, degree := range indeg {
		if degree == 0 {
			current = append(current, id)
		}
	}

	var levels [][]string
	done := 0 // 记录成功完成拓扑分层的任务总数

	// 第 4 步：逐层剥离依赖并收集下一层就绪节点
	for len(current) > 0 {
		sort.Strings(current) // 排序确保分层输出结果稳定，便于单元测试与确定性重放
		level := append([]string(nil), current...)
		levels = append(levels, level)
		done += len(level)

		var next []string
		// 模拟本层任务全部执行完毕：将依赖本层任务的后续节点入度依次减 1
		for _, id := range level {
			for _, dependent := range dependents[id] {
				indeg[dependent]--
				// 当后续节点的依赖数减至 0 时，意味着它已具备执行条件，加入下一层
				if indeg[dependent] == 0 {
					next = append(next, dependent)
				}
			}
		}
		current = next
	}

	// 第 5 步：环路检测。如果分层任务数小于总任务数，说明剩余节点陷入了循环依赖无法解套
	if done != len(indeg) {
		return nil, fmt.Errorf("计划存在循环依赖，无法执行")
	}
	return levels, nil
}
