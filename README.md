# audience — 受众匹配库

Go 语言实现的进程内受众匹配库，由上层竞价引擎直接以包形式调用，无接口层、无外部服务依赖。

## 能力

- `Load(rules []Rule) (*Engine, error)`：载入一批广告规则并编译建索引。每条规则含唯一整数广告 ID 与布尔表达式（标签 / AND / OR / NOT）。载入后规则不可变。
- `Match(tags []uint32) []int64`：返回标签集合下表达式为真的全部广告 ID（升序、不截断）。空标签集合也可能命中（如恒真表达式）。
- `Explain(adID int64, tags []uint32) (Explanation, bool)`：给出指定广告在本次请求下的完整求值轨迹，覆盖原表达式中每个标签与运算符的出现位置（先序编号），相同子表达式在不同位置分别保留，不做短路省略；根位置结果与 `Match` 一致。命中与未命中广告均可解释。

## 用法示例

```go
e, err := audience.Load([]audience.Rule{
    {AdID: 1, Expr: audience.And(audience.Tag(101), audience.Or(audience.Tag(202), audience.Not(audience.Tag(303))))},
    {AdID: 2, Expr: audience.Not(audience.Tag(101))},
})
if err != nil { /* 非法表达式或重复 ID */ }

hits := e.Match([]uint32{101, 202})      // [1]
exp, ok := e.Explain(2, []uint32{101})   // ok=true, exp.Result=false, exp.Nodes 含每个位置真值
```

表达式也可用 `&audience.ExprNode{Op: ..., Tag: ..., Args: ...}` 直接构造。

## 实现要点

- 表达式编译为扁平数组（先序编号），节点定长、子节点连续存放，100 万标签 × 600 万出现位置下内存可控。
- 匹配走倒排索引（标签 → 含该标签的广告）加"全假赋值下为真"的常驻候选集：既避免扫描全量广告，又保证 `NOT` 等否定条件在空交集时不漏判。
- 解释为独立的全量求值路径，逐位置记录真值，与匹配结果同源一致。

## 测试

```sh
go test ./...          # 全部正确性 + 规模测试
go test -run TestScale -v .   # 查看规模实测数据
```

覆盖：嵌套 NOT、恒真/恒假、重复子表达式、连续不同请求互不干扰、32 组二选一规则（每组至少一个标签命中、任一组全缺不命中）、随机表达式与逐条直接求值对照（命中集合与解释每个位置真值）。

本机实测（Apple Silicon，Go 1.27）：100000 条广告、574 万出现位置、100 万标签空间——载入约 1.0 秒（上限 30 秒），1000 次匹配共约 86 毫秒、单次最大约 0.5 毫秒（上限 10 秒 / 100 毫秒），含输入数据在内的进程峰值 RSS 约 561 MiB（上限 1 GiB）。
