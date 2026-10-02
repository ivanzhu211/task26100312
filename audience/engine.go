package audience

import "sort"

// Ad 是一条已筛选为可投放状态的广告规则。
type Ad struct {
	ID   int64 // 唯一广告 ID
	Expr *Expr // 受众布尔表达式
}

// Engine 保存一批载入后不再修改的广告规则。
// 仅支持顺序调用；Match/Explain 不修改任何内部状态。
type Engine struct {
	exprs map[int64]*Expr   // 广告 ID -> 表达式
	index map[int64][]int64 // 标签 ID -> 包含该标签的广告 ID（升序、去重）
	// alwaysTrue 保存“全部标签为假时表达式仍为真”的广告。
	// 这类广告在请求未涉及其任何标签时必然命中，无需逐条求值。
	alwaysTrue []int64
}

// NewEngine 创建一个空引擎。
func NewEngine() *Engine { return &Engine{} }

// Load 载入一批广告规则，载入后规则不再修改。
// 输入切片及其表达式在载入后归引擎所有，调用方不得再修改。
func (e *Engine) Load(ads []Ad) error {
	e.exprs = make(map[int64]*Expr, len(ads))
	e.index = make(map[int64][]int64)
	e.alwaysTrue = e.alwaysTrue[:0]

	empty := truthSet{}
	for _, ad := range ads {
		e.exprs[ad.ID] = ad.Expr
		collectTags(ad.ID, ad.Expr, e.index)
		if eval(ad.Expr, empty) {
			e.alwaysTrue = append(e.alwaysTrue, ad.ID)
		}
	}
	for tag, ids := range e.index {
		dedup := ids[:1]
		for _, id := range ids[1:] {
			if id != dedup[len(dedup)-1] {
				dedup = append(dedup, id)
			}
		}
		sort.Slice(dedup, func(i, j int) bool { return dedup[i] < dedup[j] })
		e.index[tag] = dedup
	}
	sort.Slice(e.alwaysTrue, func(i, j int) bool { return e.alwaysTrue[i] < e.alwaysTrue[j] })
	return nil
}

// collectTags 把表达式中每个标签出现位置登记到倒排索引（含 NOT 内部）。
func collectTags(adID int64, expr *Expr, index map[int64][]int64) {
	if expr.Kind == KindTag {
		index[expr.Tag] = append(index[expr.Tag], adID)
		return
	}
	for _, child := range expr.Children {
		collectTags(adID, child, index)
	}
}

// Match 返回所有且仅有表达式为真的广告 ID，升序、不截断。
func (e *Engine) Match(tags []int64) []int64 {
	truth := newTruthSet(tags)

	// 请求标签涉及的广告需要逐条求值；未涉及且恒真的广告直接命中。
	touched := make(map[int64]struct{})
	for _, tag := range tags {
		for _, id := range e.index[tag] {
			touched[id] = struct{}{}
		}
	}

	var matched []int64
	for id := range touched {
		if eval(e.exprs[id], truth) {
			matched = append(matched, id)
		}
	}
	for _, id := range e.alwaysTrue {
		if _, ok := touched[id]; !ok {
			matched = append(matched, id)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i] < matched[j] })
	return matched
}
