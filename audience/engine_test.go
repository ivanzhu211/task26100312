package audience

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// refEval 是测试用的逐条直接求值参考实现。
func refEval(expr *Expr, truth map[int64]bool) bool {
	switch expr.Kind {
	case KindTag:
		return truth[expr.Tag]
	case KindAnd:
		for _, c := range expr.Children {
			if !refEval(c, truth) {
				return false
			}
		}
		return true
	case KindOr:
		for _, c := range expr.Children {
			if refEval(c, truth) {
				return true
			}
		}
		return false
	case KindNot:
		return !refEval(expr.Children[0], truth)
	}
	panic("unknown kind")
}

func refTruth(tags []int64) map[int64]bool {
	m := make(map[int64]bool, len(tags))
	for _, t := range tags {
		m[t] = true
	}
	return m
}

func refMatch(ads []Ad, tags []int64) []int64 {
	truth := refTruth(tags)
	var got []int64
	for _, ad := range ads {
		if refEval(ad.Expr, truth) {
			got = append(got, ad.ID)
		}
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	return got
}

// checkExplain 校验解释树与原表达式逐位置同构，且每个位置真值与参考求值一致。
func checkExplain(t *testing.T, expr *Expr, node *ExplainedNode, truth map[int64]bool) {
	t.Helper()
	if node == nil {
		t.Fatalf("解释树缺少位置: kind=%v tag=%d", expr.Kind, expr.Tag)
	}
	if node.Kind != expr.Kind || node.Tag != expr.Tag {
		t.Fatalf("位置结构不一致: got kind=%v tag=%d, want kind=%v tag=%d",
			node.Kind, node.Tag, expr.Kind, expr.Tag)
	}
	if want := refEval(expr, truth); node.Value != want {
		t.Fatalf("位置真值不一致: kind=%v tag=%d got=%v want=%v",
			expr.Kind, expr.Tag, node.Value, want)
	}
	if len(node.Children) != len(expr.Children) {
		t.Fatalf("子位置数量不一致: got=%d want=%d", len(node.Children), len(expr.Children))
	}
	for i := range expr.Children {
		checkExplain(t, expr.Children[i], node.Children[i], truth)
	}
}

func loadEngine(t *testing.T, ads []Ad) *Engine {
	t.Helper()
	e := NewEngine()
	if err := e.Load(ads); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return e
}

func TestNestedNot(t *testing.T) {
	// NOT (NOT (NOT tag1)) == NOT tag1
	ads := []Ad{
		{ID: 1, Expr: Not(Not(Not(Tag(1))))},
		{ID: 2, Expr: Not(Not(Tag(1)))},
	}
	e := loadEngine(t, ads)
	if got := e.Match([]int64{1}); !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("Match({1}) = %v, want [2]", got)
	}
	if got := e.Match(nil); !reflect.DeepEqual(got, []int64{1}) {
		t.Fatalf("Match({}) = %v, want [1]", got)
	}
}

func TestAlwaysTrueAndFalse(t *testing.T) {
	// 恒真: OR(tag, NOT tag)；恒假: AND(tag, NOT tag)
	ads := []Ad{
		{ID: 1, Expr: Or(Tag(7), Not(Tag(7)))},
		{ID: 2, Expr: And(Tag(8), Not(Tag(8)))},
		{ID: 3, Expr: Tag(9)},
	}
	e := loadEngine(t, ads)
	for _, tags := range [][]int64{nil, {7}, {8}, {9}, {7, 8, 9}, {100}} {
		want := refMatch(ads, tags)
		if got := e.Match(tags); !reflect.DeepEqual(got, want) {
			t.Fatalf("Match(%v) = %v, want %v", tags, got, want)
		}
	}
}

func TestEmptyTagSet(t *testing.T) {
	ads := []Ad{
		{ID: 1, Expr: Not(Tag(1))},
		{ID: 2, Expr: And(Not(Tag(1)), Not(Tag(2)))},
		{ID: 3, Expr: Tag(3)},
	}
	e := loadEngine(t, ads)
	if got := e.Match(nil); !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Fatalf("Match(nil) = %v, want [1 2]", got)
	}
}

func TestRepeatedSubExpr(t *testing.T) {
	// 相同子表达式在不同位置重复出现
	sub := func() *Expr { return And(Tag(1), Not(Tag(2))) }
	ads := []Ad{{ID: 1, Expr: Or(sub(), sub(), Tag(3))}}
	e := loadEngine(t, ads)
	for _, tags := range [][]int64{nil, {1}, {2}, {1, 2}, {3}} {
		want := refMatch(ads, tags)
		if got := e.Match(tags); !reflect.DeepEqual(got, want) {
			t.Fatalf("Match(%v) = %v, want %v", tags, got, want)
		}
		ex := e.Explain(1, tags)
		if ex == nil || ex.Matched != (len(want) == 1) {
			t.Fatalf("Explain root 与 Match 不一致: tags=%v", tags)
		}
		checkExplain(t, ads[0].Expr, ex.Root, refTruth(tags))
	}
}

func TestSequentialRequestsIndependent(t *testing.T) {
	ads := []Ad{
		{ID: 1, Expr: And(Tag(1), Not(Tag(2)))},
		{ID: 2, Expr: Or(Tag(2), Tag(3))},
		{ID: 3, Expr: Not(Tag(1))},
	}
	e := loadEngine(t, ads)
	requests := [][]int64{{1}, {1, 2}, {2}, nil, {3}, {1, 3}, {1, 2, 3}, nil, {1}}
	for i, tags := range requests {
		want := refMatch(ads, tags)
		if got := e.Match(tags); !reflect.DeepEqual(got, want) {
			t.Fatalf("第 %d 次请求 Match(%v) = %v, want %v", i, tags, got, want)
		}
	}
	// 重复第一次请求，结果必须与首次一致
	if got := e.Match([]int64{1}); !reflect.DeepEqual(got, []int64{1}) {
		t.Fatalf("重复请求结果漂移: %v", got)
	}
}

func TestExplainHitAndMiss(t *testing.T) {
	expr := And(Or(Tag(1), Tag(2)), Not(And(Tag(3), Not(Tag(4)))))
	ads := []Ad{{ID: 42, Expr: expr}}
	e := loadEngine(t, ads)
	for _, tags := range [][]int64{nil, {1}, {2, 3}, {1, 3, 4}, {2, 3}, {1, 2, 3, 4}} {
		ex := e.Explain(42, tags)
		if ex == nil {
			t.Fatalf("Explain 返回 nil: tags=%v", tags)
		}
		matched := len(e.Match(tags)) == 1
		if ex.Matched != matched || ex.Root.Value != matched {
			t.Fatalf("根位置与匹配结果脱节: tags=%v matched=%v ex=%v", tags, matched, ex.Matched)
		}
		checkExplain(t, expr, ex.Root, refTruth(tags))
	}
	if e.Explain(999, nil) != nil {
		t.Fatal("不存在的广告应返回 nil")
	}
}

// randExpr 生成嵌套不超过 depth、位置数不超过 budget 的随机合法表达式。
func randExpr(r *rand.Rand, depth, budget int, maxTag int64) *Expr {
	if depth == 0 || budget < 3 || r.Intn(3) == 0 {
		return Tag(r.Int63n(maxTag) + 1)
	}
	switch r.Intn(3) {
	case 0:
		return Not(randExpr(r, depth-1, budget-1, maxTag))
	case 1:
		n := 2 + r.Intn(3)
		children := make([]*Expr, n)
		for i := range children {
			children[i] = randExpr(r, depth-1, budget/n, maxTag)
		}
		return And(children...)
	default:
		n := 2 + r.Intn(3)
		children := make([]*Expr, n)
		for i := range children {
			children[i] = randExpr(r, depth-1, budget/n, maxTag)
		}
		return Or(children...)
	}
}

func TestRandomizedAgainstReference(t *testing.T) {
	r := rand.New(rand.NewSource(20261002))
	const numAds = 2000
	ads := make([]Ad, numAds)
	for i := range ads {
		ads[i] = Ad{ID: int64(i + 1), Expr: randExpr(r, 6, 40, 300)}
	}
	e := loadEngine(t, ads)
	for round := 0; round < 200; round++ {
		n := r.Intn(20)
		tags := make([]int64, n)
		for i := range tags {
			tags[i] = r.Int63n(300) + 1
		}
		want := refMatch(ads, tags)
		if got := e.Match(tags); !reflect.DeepEqual(got, want) {
			t.Fatalf("第 %d 轮 Match(%v) 不一致:\ngot  %v\nwant %v", round, tags, got, want)
		}
		// 每轮抽查若干广告的解释，含命中与未命中
		truth := refTruth(tags)
		for _, id := range []int64{int64(r.Intn(numAds) + 1), int64(r.Intn(numAds) + 1)} {
			ex := e.Explain(id, tags)
			checkExplain(t, ads[id-1].Expr, ex.Root, truth)
			if ex.Matched != refEval(ads[id-1].Expr, truth) {
				t.Fatalf("广告 %d 解释根值与参考求值不一致", id)
			}
		}
	}
}

// TestThirtyTwoPairs 覆盖 32 组二选一条件：每组至少一个标签时命中，
// 任意一组两个标签都缺失时不命中。
func TestThirtyTwoPairs(t *testing.T) {
	const groups = 32
	children := make([]*Expr, groups)
	for i := 0; i < groups; i++ {
		children[i] = Or(Tag(int64(2*i+1)), Tag(int64(2*i+2)))
	}
	expr := And(children...)
	ads := []Ad{{ID: 1, Expr: expr}}
	e := loadEngine(t, ads)

	// 每组取第一个标签：命中
	full := make([]int64, groups)
	for i := range full {
		full[i] = int64(2*i + 1)
	}
	if got := e.Match(full); !reflect.DeepEqual(got, []int64{1}) {
		t.Fatalf("全组覆盖应命中: %v", got)
	}
	// 任意一组两个标签都缺失：不命中
	for drop := 0; drop < groups; drop++ {
		tags := make([]int64, 0, groups-1)
		for i := 0; i < groups; i++ {
			if i != drop {
				tags = append(tags, int64(2*i+1))
			}
		}
		if got := e.Match(tags); len(got) != 0 {
			t.Fatalf("缺失第 %d 组不应命中: %v", drop, got)
		}
		ex := e.Explain(1, tags)
		if ex.Matched {
			t.Fatalf("缺失第 %d 组解释根值应为 false", drop)
		}
		checkExplain(t, expr, ex.Root, refTruth(tags))
	}
}
