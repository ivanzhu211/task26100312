package audience

// truthSet 是一次请求中真值标签的集合。
type truthSet map[int64]struct{}

// newTruthSet 把请求携带的标签集合转为真值集合，顺序与重复不影响结果。
func newTruthSet(tags []int64) truthSet {
	set := make(truthSet, len(tags))
	for _, tag := range tags {
		set[tag] = struct{}{}
	}
	return set
}

// eval 对表达式做完整求值。
// 匹配路径允许 AND/OR 短路；解释路径需要每个位置的真值，使用 evalFull。
func eval(expr *Expr, truth truthSet) bool {
	switch expr.Kind {
	case KindTag:
		_, ok := truth[expr.Tag]
		return ok
	case KindAnd:
		for _, child := range expr.Children {
			if !eval(child, truth) {
				return false
			}
		}
		return true
	case KindOr:
		for _, child := range expr.Children {
			if eval(child, truth) {
				return true
			}
		}
		return false
	case KindNot:
		return !eval(expr.Children[0], truth)
	}
	return false
}

// evalFull 完整求值并为每个出现位置记录真值，返回解释树。
// 不使用短路，保证相同子表达式在不同位置出现时分别保留真值。
func evalFull(expr *Expr, truth truthSet) *ExplainedNode {
	node := &ExplainedNode{Kind: expr.Kind, Tag: expr.Tag}
	switch expr.Kind {
	case KindTag:
		_, node.Value = truth[expr.Tag]
	case KindAnd:
		node.Value = true
		node.Children = make([]*ExplainedNode, len(expr.Children))
		for i, child := range expr.Children {
			node.Children[i] = evalFull(child, truth)
			node.Value = node.Value && node.Children[i].Value
		}
	case KindOr:
		node.Value = false
		node.Children = make([]*ExplainedNode, len(expr.Children))
		for i, child := range expr.Children {
			node.Children[i] = evalFull(child, truth)
			node.Value = node.Value || node.Children[i].Value
		}
	case KindNot:
		node.Children = make([]*ExplainedNode, 1)
		node.Children[0] = evalFull(expr.Children[0], truth)
		node.Value = !node.Children[0].Value
	}
	return node
}
