package audience

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// refEval 直接对原始表达式树求值，作为对照实现。
func refEval(n *ExprNode, has func(uint32) bool) bool {
	switch n.Op {
	case OpTag:
		return has(n.Tag)
	case OpNot:
		return !refEval(n.Args[0], has)
	case OpAnd:
		v := true
		for _, a := range n.Args {
			if !refEval(a, has) {
				v = false
			}
		}
		return v
	default:
		v := false
		for _, a := range n.Args {
			if refEval(a, has) {
				v = true
			}
		}
		return v
	}
}

func tagSetOf(tags []uint32) func(uint32) bool {
	m := make(map[uint32]bool, len(tags))
	for _, t := range tags {
		m[t] = true
	}
	return func(t uint32) bool { return m[t] }
}

// checkExplain 校验解释：覆盖每个出现位置、位置真值与直接求值一致、根与 Match 一致。
func checkExplain(t *testing.T, e *Engine, adID int64, root *ExprNode, tags []uint32) {
	t.Helper()
	exp, ok := e.Explain(adID, tags)
	if !ok {
		t.Fatalf("广告 %d 无法解释", adID)
	}
	has := tagSetOf(tags)
	// 先序展开原始树，逐位置对照。
	var flat []*ExprNode
	var walk func(n *ExprNode)
	walk = func(n *ExprNode) {
		flat = append(flat, n)
		for _, a := range n.Args {
			walk(a)
		}
	}
	walk(root)
	if len(exp.Nodes) != len(flat) {
		t.Fatalf("广告 %d: 解释位置数 %d, 期望 %d", adID, len(exp.Nodes), len(flat))
	}
	for i, n := range flat {
		got := exp.Nodes[i]
		if got.Position != i {
			t.Fatalf("广告 %d: 位置 %d 的 Position=%d", adID, i, got.Position)
		}
		if got.Op != n.Op || got.Tag != n.Tag {
			t.Fatalf("广告 %d: 位置 %d 节点不符", adID, i)
		}
		if len(got.Children) != len(n.Args) {
			t.Fatalf("广告 %d: 位置 %d 子节点数 %d, 期望 %d", adID, i, len(got.Children), len(n.Args))
		}
		want := refEval(n, has)
		if got.Value != want {
			t.Fatalf("广告 %d: 位置 %d 真值=%v, 期望 %v", adID, i, got.Value, want)
		}
	}
	if exp.Result != refEval(root, has) {
		t.Fatalf("广告 %d: 根结果 %v 与直接求值不符", adID, exp.Result)
	}
	matched := false
	for _, id := range e.Match(tags) {
		if id == adID {
			matched = true
		}
	}
	if exp.Result != matched {
		t.Fatalf("广告 %d: 解释根结果 %v 与 Match 结果 %v 脱节", adID, exp.Result, matched)
	}
}

func TestNestedNot(t *testing.T) {
	// NOT(NOT(NOT(tag1))) == NOT(tag1)
	expr := Not(Not(Not(Tag(1))))
	e, err := Load([]Rule{{AdID: 1, Expr: expr}})
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Match(nil); !reflect.DeepEqual(got, []int64{1}) {
		t.Fatalf("空标签应命中, got %v", got)
	}
	if got := e.Match([]uint32{1}); len(got) != 0 {
		t.Fatalf("含标签1不应命中, got %v", got)
	}
	checkExplain(t, e, 1, expr, nil)
	checkExplain(t, e, 1, expr, []uint32{1})
}

func TestAlwaysTrueFalse(t *testing.T) {
	alwaysTrue := Or(Tag(7), Not(Tag(7)))
	alwaysFalse := And(Tag(8), Not(Tag(8)))
	e, err := Load([]Rule{
		{AdID: 10, Expr: alwaysTrue},
		{AdID: 20, Expr: alwaysFalse},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tags := range [][]uint32{nil, {7}, {8}, {7, 8}, {99}} {
		got := e.Match(tags)
		if !reflect.DeepEqual(got, []int64{10}) {
			t.Fatalf("tags=%v 应只命中恒真广告, got %v", tags, got)
		}
		checkExplain(t, e, 10, alwaysTrue, tags)
		checkExplain(t, e, 20, alwaysFalse, tags)
	}
}

func TestRepeatedSubexpr(t *testing.T) {
	// 相同子表达式出现在不同位置，解释须分别保留。
	sub := And(Tag(1), Tag(2))
	expr := Or(sub, sub, Not(sub))
	e, err := Load([]Rule{{AdID: 5, Expr: expr}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tags := range [][]uint32{nil, {1}, {1, 2}} {
		checkExplain(t, e, 5, expr, tags)
	}
	exp, _ := e.Explain(5, []uint32{1, 2})
	if len(exp.Nodes) != 11 { // Or + (And+2Tag) + (And+2Tag) + (Not+And+2Tag)
		t.Fatalf("重复子表达式应分别保留位置, 共 11 个, got %d", len(exp.Nodes))
	}
}

func TestSequentialRequests(t *testing.T) {
	rules := []Rule{
		{AdID: 1, Expr: And(Tag(1), Tag(2))},
		{AdID: 2, Expr: Or(Tag(1), Tag(3))},
		{AdID: 3, Expr: Not(Tag(1))},
	}
	e, err := Load(rules)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		tags []uint32
		want []int64
	}{
		{[]uint32{1, 2}, []int64{1, 2}},
		{[]uint32{3}, []int64{2, 3}},
		{nil, []int64{3}},
		{[]uint32{1}, []int64{2}},
		{[]uint32{2, 2, 2}, []int64{3}}, // 重复标签不影响结果
		{[]uint32{1, 2}, []int64{1, 2}}, // 重复同一请求结果一致
	}
	for i, c := range cases {
		if got := e.Match(c.tags); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("第 %d 次请求 tags=%v: got %v, want %v", i, c.tags, got, c.want)
		}
	}
}

func TestThirtyTwoGroups(t *testing.T) {
	// 32 组二选一：每组至少一个标签时命中，任一组两个都缺失则不命中。
	args := make([]*ExprNode, 32)
	for i := range args {
		args[i] = Or(Tag(uint32(2*i+1)), Tag(uint32(2*i+2)))
	}
	expr := And(args...)
	e, err := Load([]Rule{{AdID: 100, Expr: expr}})
	if err != nil {
		t.Fatal(err)
	}
	full := make([]uint32, 0, 32)
	for i := 0; i < 32; i++ {
		full = append(full, uint32(2*i+1))
	}
	if got := e.Match(full); !reflect.DeepEqual(got, []int64{100}) {
		t.Fatalf("每组都有标签应命中, got %v", got)
	}
	// 缺第 5 组的两个标签（用组内另一个标签补齐其余组）。
	missing := make([]uint32, 0, 32)
	for i := 0; i < 32; i++ {
		if i == 5 {
			continue
		}
		missing = append(missing, uint32(2*i+2))
	}
	if got := e.Match(missing); len(got) != 0 {
		t.Fatalf("第 5 组全缺不应命中, got %v", got)
	}
	checkExplain(t, e, 100, expr, full)
	checkExplain(t, e, 100, expr, missing)
}

// TestRandomConsistency 随机表达式 + 随机请求：Match 与逐条直接求值一致，
// 解释每个位置真值与直接求值一致。
func TestRandomConsistency(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const numAds = 300
	rules := make([]Rule, numAds)
	exprs := make([]*ExprNode, numAds)
	for i := range rules {
		exprs[i] = randomExpr(rng, 1)
		rules[i] = Rule{AdID: int64(1000 + i), Expr: exprs[i]}
	}
	e, err := Load(rules)
	if err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 50; round++ {
		var tags []uint32
		for _, v := range rng.Perm(60)[:rng.Intn(12)] {
			tags = append(tags, uint32(v+1))
		}
		has := tagSetOf(tags)
		var want []int64
		for i, r := range rules {
			if refEval(exprs[i], has) {
				want = append(want, r.AdID)
			}
		}
		sort.Slice(want, func(a, b int) bool { return want[a] < want[b] })
		got := e.Match(tags)
		if len(want) == 0 && len(got) == 0 {
			// 空集合等价
		} else if !reflect.DeepEqual(got, want) {
			t.Fatalf("round %d tags=%v: got %v, want %v", round, tags, got, want)
		}
		// 抽查若干广告的解释。
		for _, i := range rng.Perm(numAds)[:5] {
			checkExplain(t, e, rules[i].AdID, exprs[i], tags)
		}
	}
}

// randomExpr 生成随机合法表达式，控制嵌套与规模。
func randomExpr(rng *rand.Rand, depth int) *ExprNode {
	if depth >= 6 || rng.Intn(3) == 0 {
		return Tag(uint32(rng.Intn(60) + 1))
	}
	switch rng.Intn(3) {
	case 0:
		return Not(randomExpr(rng, depth+1))
	case 1:
		n := 2 + rng.Intn(3)
		args := make([]*ExprNode, n)
		for i := range args {
			args[i] = randomExpr(rng, depth+1)
		}
		return And(args...)
	default:
		n := 2 + rng.Intn(3)
		args := make([]*ExprNode, n)
		for i := range args {
			args[i] = randomExpr(rng, depth+1)
		}
		return Or(args...)
	}
}

func TestExplainUnknownAd(t *testing.T) {
	e, err := Load([]Rule{{AdID: 1, Expr: Tag(1)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := e.Explain(999, nil); ok {
		t.Fatal("不存在的广告应返回 ok=false")
	}
}

func TestInvalidInput(t *testing.T) {
	bad := []Rule{
		{AdID: 1, Expr: &ExprNode{Op: OpAnd, Args: []*ExprNode{Tag(1)}}}, // AND 只有一个操作数
		{AdID: 2, Expr: &ExprNode{Op: OpTag, Tag: 0}},                    // 标签 ID 非正
		{AdID: 3, Expr: &ExprNode{Op: OpNot}},                            // NOT 无操作数
	}
	for _, r := range bad {
		if _, err := Load([]Rule{r}); err == nil {
			t.Fatalf("非法表达式应报错: %+v", r.Expr)
		}
	}
	if _, err := Load([]Rule{
		{AdID: 1, Expr: Tag(1)},
		{AdID: 1, Expr: Tag(2)},
	}); err == nil {
		t.Fatal("重复广告 ID 应报错")
	}
}
