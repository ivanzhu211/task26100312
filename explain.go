package audience

// NodeExplanation 描述原表达式中一个出现位置（先序编号）的求值结果。
type NodeExplanation struct {
	Position int    // 先序位置编号，从 0 开始
	Op       Op     // 该位置的运算符
	Tag      uint32 // OpTag 时的标签 ID
	Value    bool   // 该位置子表达式在本次请求下的真值
	Children []int  // 子节点的位置编号
}

// Explanation 是某条广告在一次请求下的完整求值轨迹，
// 覆盖表达式中每个标签与运算符的出现位置（非短路）。
type Explanation struct {
	AdID   int64
	Result bool // 根位置真值，与 Match 结果一致
	Nodes  []NodeExplanation
}

// Explain 返回指定广告在标签集合下的完整解释。
// 命中与未命中的广告均可解释；广告不存在时 ok 为 false。
func (e *Engine) Explain(adID int64, tags []uint32) (Explanation, bool) {
	idx, ok := e.adIndex[adID]
	if !ok {
		return Explanation{}, false
	}
	tagSet := make(map[uint32]struct{}, len(tags))
	for _, t := range tags {
		tagSet[t] = struct{}{}
	}
	has := func(t uint32) bool {
		_, ok := tagSet[t]
		return ok
	}
	ce := &e.exprs[idx]
	exp := Explanation{
		AdID:  adID,
		Nodes: make([]NodeExplanation, len(ce.ops)),
	}
	for i := range ce.ops {
		start := ce.childStart[i]
		end := start + uint32(ce.childCount[i])
		children := make([]int, 0, end-start)
		for j := start; j < end; j++ {
			children = append(children, int(ce.childIdx[j]))
		}
		exp.Nodes[i] = NodeExplanation{
			Position: i,
			Op:       ce.ops[i],
			Tag:      ce.tags[i],
			Children: children,
		}
	}
	exp.Result = explainAt(ce, 0, has, exp.Nodes)
	return exp, true
}

// explainAt 对 node 处的子表达式完整求值，把每个位置的真值写入 nodes。
// 不使用短路，保证所有出现位置都有真值。
func explainAt(ce *compiledExpr, node int32, has func(uint32) bool, nodes []NodeExplanation) bool {
	var value bool
	switch ce.ops[node] {
	case OpTag:
		value = has(ce.tags[node])
	case OpNot:
		value = !explainAt(ce, ce.childIdx[ce.childStart[node]], has, nodes)
	case OpAnd:
		value = true
		end := ce.childEnd(node)
		for i := ce.childStart[node]; i < end; i++ {
			if !explainAt(ce, ce.childIdx[i], has, nodes) {
				value = false
			}
		}
	default: // OpOr
		value = false
		end := ce.childEnd(node)
		for i := ce.childStart[node]; i < end; i++ {
			if explainAt(ce, ce.childIdx[i], has, nodes) {
				value = true
			}
		}
	}
	nodes[node].Value = value
	return value
}
