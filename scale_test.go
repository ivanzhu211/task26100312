package audience

import (
	"math/rand"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// maxRSS 返回进程峰值常驻内存（字节）。
func maxRSS() uint64 {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	if runtime.GOOS == "darwin" {
		return uint64(ru.Maxrss) // darwin 单位为字节
	}
	return uint64(ru.Maxrss) * 1024 // linux 单位为 KB
}

// makeScaleExpr 生成 57 个出现位置的表达式：
// And(Or(11 标签), Or(11 标签), Not(Or(8 标签)), Or(11 标签), Not(Or(8 标签)))。
func makeScaleExpr(rng *rand.Rand, tagSpace uint32) *ExprNode {
	orTags := func(n int) *ExprNode {
		args := make([]*ExprNode, n)
		for i := range args {
			args[i] = Tag(uint32(rng.Intn(int(tagSpace)) + 1))
		}
		return Or(args...)
	}
	return And(orTags(11), orTags(11), Not(orTags(8)), orTags(11), Not(orTags(8)))
}

// TestScale 达到验收规模：100000 条广告、100 万标签空间、约 580 万出现位置。
// 报告载入时间、1000 次匹配耗时与峰值内存，并抽样与直接求值对照。
func TestScale(t *testing.T) {
	const (
		numAds   = 100000
		tagSpace = 1000000
		numReqs  = 1000
	)
	rng := rand.New(rand.NewSource(7))
	rules := make([]Rule, 0, numAds)
	positions := 0
	var countPos func(n *ExprNode) int
	countPos = func(n *ExprNode) int {
		c := 1
		for _, a := range n.Args {
			c += countPos(a)
		}
		return c
	}
	for i := 0; i < numAds-1000; i++ {
		expr := makeScaleExpr(rng, tagSpace)
		positions += countPos(expr)
		rules = append(rules, Rule{AdID: int64(i + 1), Expr: expr})
	}
	// 1000 条 32 组二选一规则（每组标签按广告错开，便于构造命中/未命中请求）。
	groupBase := uint32(900000)
	for i := 0; i < 1000; i++ {
		args := make([]*ExprNode, 32)
		for g := range args {
			base := groupBase + uint32((i*32+g)*2)%(tagSpace-groupBase)
			args[g] = Or(Tag(base), Tag(base+1))
		}
		expr := And(args...)
		positions += countPos(expr)
		rules = append(rules, Rule{AdID: int64(numAds - 1000 + i + 1), Expr: expr})
	}
	t.Logf("广告数=%d 出现位置总数=%d", len(rules), positions)

	runtime.GC()
	start := time.Now()
	e, err := Load(rules)
	loadDur := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("载入耗时=%v 峰值RSS=%.1fMiB", loadDur, float64(maxRSS())/1048576)
	if loadDur > 30*time.Second {
		t.Fatalf("载入超过 30 秒: %v", loadDur)
	}

	// 构造 1000 个请求：每个至多 50 个标签。
	reqs := make([][]uint32, numReqs)
	for i := range reqs {
		n := 1 + rng.Intn(50)
		tags := make([]uint32, n)
		for j := range tags {
			tags[j] = uint32(rng.Intn(tagSpace) + 1)
		}
		reqs[i] = tags
	}
	// 一个针对 32 组规则的命中请求（每组取第一个标签）与一个未命中请求（缺第 0 组）。
	hitTags := make([]uint32, 0, 32)
	for g := 0; g < 32; g++ {
		hitTags = append(hitTags, groupBase+uint32(g*2)%(tagSpace-groupBase))
	}
	missTags := hitTags[2:]
	reqs[0] = hitTags
	reqs[1] = missTags

	matchStart := time.Now()
	var maxSingle time.Duration
	results := make([][]int64, numReqs)
	for i, tags := range reqs {
		s := time.Now()
		results[i] = e.Match(tags)
		if d := time.Since(s); d > maxSingle {
			maxSingle = d
		}
	}
	totalMatch := time.Since(matchStart)
	t.Logf("1000 次匹配总耗时=%v 单次最大=%v 平均=%v", totalMatch, maxSingle, totalMatch/numReqs)
	t.Logf("结束后峰值RSS=%.1fMiB", float64(maxRSS())/1048576)
	if totalMatch > 10*time.Second {
		t.Fatalf("1000 次匹配超过 10 秒: %v", totalMatch)
	}
	if maxSingle > 100*time.Millisecond {
		t.Fatalf("单次匹配超过 100ms: %v", maxSingle)
	}

	// 32 组规则：广告 numAds-1000+1 的组标签从 groupBase 起，应被 hitTags 命中。
	found := false
	for _, id := range results[0] {
		if id == int64(numAds-1000+1) {
			found = true
		}
	}
	if !found {
		t.Fatal("32 组规则广告在每组都有标签时应命中")
	}
	for _, id := range results[1] {
		if id == int64(numAds-1000+1) {
			t.Fatal("32 组规则广告在第 0 组全缺时不应命中")
		}
	}

	// 抽样 10 个请求、每个请求抽样 50 条广告，与直接求值对照。
	for _, ri := range []int{0, 1, 2, 500, 999} {
		has := tagSetOf(reqs[ri])
		hit := make(map[int64]bool, len(results[ri]))
		for _, id := range results[ri] {
			hit[id] = true
		}
		for _, ai := range rng.Perm(len(rules))[:50] {
			want := refEval(rules[ai].Expr, has)
			if got := hit[rules[ai].AdID]; got != want {
				t.Fatalf("请求 %d 广告 %d: Match=%v 直接求值=%v", ri, rules[ai].AdID, got, want)
			}
		}
	}
	// 解释抽查：命中与未命中各若干。
	for _, ai := range rng.Perm(len(rules))[:20] {
		checkExplain(t, e, rules[ai].AdID, rules[ai].Expr, reqs[2])
	}
}
