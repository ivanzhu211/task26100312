package audience

// ExplainedNode 是表达式某个出现位置的求值结果。
// 树形结构与原表达式一一对应，覆盖每个标签和运算符的出现位置。
type ExplainedNode struct {
	Kind     NodeKind         // 节点类型
	Tag      int64            // 仅 KindTag 有效
	Value    bool             // 该位置在本次请求下的真值
	Children []*ExplainedNode // 子位置，顺序与原表达式一致
}

// Explanation 是指定广告在一次请求下的完整解释。
type Explanation struct {
	AdID    int64          // 广告 ID
	Matched bool           // 根位置真值，与 Match 结果一致
	Root    *ExplainedNode // 覆盖原表达式全部出现位置的解释树
}

// Explain 解释指定广告在给定标签集合下为何匹配或不匹配。
// 命中与未命中的广告均可解释；广告不存在时返回 nil。
// 解释树覆盖原表达式的每个出现位置，不做短路裁剪。
func (e *Engine) Explain(adID int64, tags []int64) *Explanation {
	expr, ok := e.exprs[adID]
	if !ok {
		return nil
	}
	root := evalFull(expr, newTruthSet(tags))
	return &Explanation{
		AdID:    adID,
		Matched: root.Value,
		Root:    root,
	}
}
