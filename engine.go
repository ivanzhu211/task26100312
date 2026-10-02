package audience

import (
	"fmt"
	"sort"
)

// Rule 是一条广告规则：唯一广告 ID 加布尔表达式。
type Rule struct {
	AdID int64
	Expr *ExprNode
}

// compiledExpr 以扁平数组保存一条表达式，节点按先序编号（与解释位置一致）。
type compiledExpr struct {
	ops        []Op     // 每个位置的运算符
	tags       []uint32 // OpTag 位置的标签 ID
	childStart []uint32 // 子节点在 childIdx 中的起始下标
	childCount []uint16 // 子节点个数
	childIdx   []int32  // 各节点的子节点位置，按先序连续存放
}

// Engine 保存一批已编译的规则，提供匹配与解释能力。
// 载入后规则不可变，可安全地串行用于多次请求。
type Engine struct {
	exprs       []compiledExpr     // 按广告下标存放的编译结果
	adIDs       []int64            // 下标对应的广告 ID
	adIndex     map[int64]int      // 广告 ID -> 下标
	postings    map[uint32][]int32 // 标签 -> 含该标签的广告下标（每条广告每标签只出现一次）
	alwaysMatch []int32            // 全假赋值下仍为真的广告（任何请求都需评估）
}

// compile 把表达式树编译为扁平数组，并收集去重后的标签集合。
func compile(root *ExprNode) (compiledExpr, []uint32) {
	ce := compiledExpr{}
	var tagSet []uint32
	var walk func(n *ExprNode) int32
	walk = func(n *ExprNode) int32 {
		pos := int32(len(ce.ops))
		ce.ops = append(ce.ops, n.Op)
		ce.tags = append(ce.tags, n.Tag)
		ce.childStart = append(ce.childStart, 0)
		ce.childCount = append(ce.childCount, uint16(len(n.Args)))
		if n.Op == OpTag {
			tagSet = append(tagSet, n.Tag)
		}
		childPos := make([]int32, 0, len(n.Args))
		for _, arg := range n.Args {
			childPos = append(childPos, walk(arg))
		}
		ce.childStart[pos] = uint32(len(ce.childIdx))
		ce.childIdx = append(ce.childIdx, childPos...)
		return pos
	}
	walk(root)
	// 每条表达式最多 256 个位置，标签去重成本低。
	sort.Slice(tagSet, func(i, j int) bool { return tagSet[i] < tagSet[j] })
	uniq := tagSet[:0]
	for i, t := range tagSet {
		if i == 0 || t != tagSet[i-1] {
			uniq = append(uniq, t)
		}
	}
	return ce, uniq
}

// evalAt 对编译后的表达式在 node 处求值（允许短路）。
func evalAt(ce *compiledExpr, node int32, has func(uint32) bool) bool {
	switch ce.ops[node] {
	case OpTag:
		return has(ce.tags[node])
	case OpNot:
		return !evalAt(ce, ce.childIdx[ce.childStart[node]], has)
	case OpAnd:
		end := ce.childEnd(node)
		for i := ce.childStart[node]; i < end; i++ {
			if !evalAt(ce, ce.childIdx[i], has) {
				return false
			}
		}
		return true
	default: // OpOr
		end := ce.childEnd(node)
		for i := ce.childStart[node]; i < end; i++ {
			if evalAt(ce, ce.childIdx[i], has) {
				return true
			}
		}
		return false
	}
}

// childEnd 返回 node 的子节点区间终点（childIdx 下标，不含）。
func (ce *compiledExpr) childEnd(node int32) uint32 {
	return ce.childStart[node] + uint32(ce.childCount[node])
}

// Load 编译一批广告规则并构建索引。输入表达式保证合法，
// 但仍做防御性校验；发现非法输入时返回错误。
func Load(rules []Rule) (*Engine, error) {
	e := &Engine{
		exprs:    make([]compiledExpr, len(rules)),
		adIDs:    make([]int64, len(rules)),
		adIndex:  make(map[int64]int, len(rules)),
		postings: make(map[uint32][]int32),
	}
	for i, r := range rules {
		if err := validate(r.Expr); err != nil {
			return nil, fmt.Errorf("audience: 广告 %d: %w", r.AdID, err)
		}
		if _, dup := e.adIndex[r.AdID]; dup {
			return nil, fmt.Errorf("audience: 广告 ID %d 重复", r.AdID)
		}
		ce, tags := compile(r.Expr)
		e.exprs[i] = ce
		e.adIDs[i] = r.AdID
		e.adIndex[r.AdID] = i
		for _, t := range tags {
			e.postings[t] = append(e.postings[t], int32(i))
		}
		if !evalAt(&ce, 0, func(uint32) bool { return false }) {
			continue
		}
		e.alwaysMatch = append(e.alwaysMatch, int32(i))
	}
	return e, nil
}

// Match 返回标签集合下表达式为真的全部广告 ID（升序，不截断）。
func (e *Engine) Match(tags []uint32) []int64 {
	tagSet := make(map[uint32]struct{}, len(tags))
	for _, t := range tags {
		tagSet[t] = struct{}{}
	}
	has := func(t uint32) bool {
		_, ok := tagSet[t]
		return ok
	}
	// 候选 = 含任一请求标签的广告 ∪ 全假赋值下为真的广告。
	// 不含任何请求标签且不在 alwaysMatch 中的广告，
	// 其求值结果与全假赋值一致（为假），必然不命中。
	seen := make(map[int32]struct{}, len(e.alwaysMatch))
	var result []int64
	try := func(idx int32) {
		if _, ok := seen[idx]; ok {
			return
		}
		seen[idx] = struct{}{}
		if evalAt(&e.exprs[idx], 0, has) {
			result = append(result, e.adIDs[idx])
		}
	}
	for _, idx := range e.alwaysMatch {
		try(idx)
	}
	for _, t := range tags {
		for _, idx := range e.postings[t] {
			try(idx)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
