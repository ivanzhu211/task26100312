// Package audience 提供进程内存中的受众规则匹配能力。
package audience

// NodeKind 表示表达式节点的类型。
type NodeKind uint8

const (
	KindTag NodeKind = iota // 标签叶子节点
	KindAnd                 // 合取，至少两个操作数
	KindOr                  // 析取，至少两个操作数
	KindNot                 // 否定，恰好一个操作数
)

// Expr 是受众布尔表达式的抽象语法树节点。
// 每个出现位置都是独立节点，相同子表达式在不同位置出现时各自保留。
type Expr struct {
	Kind     NodeKind // 节点类型
	Tag      int64    // 仅 KindTag 有效，正整数标签 ID
	Children []*Expr  // AND/OR 的操作数，或 NOT 的唯一操作数
}

// Tag 构造一个标签叶子节点。
func Tag(tag int64) *Expr { return &Expr{Kind: KindTag, Tag: tag} }

// And 构造合取节点，至少两个操作数。
func And(children ...*Expr) *Expr { return &Expr{Kind: KindAnd, Children: children} }

// Or 构造析取节点，至少两个操作数。
func Or(children ...*Expr) *Expr { return &Expr{Kind: KindOr, Children: children} }

// Not 构造否定节点，恰好一个操作数。
func Not(child *Expr) *Expr { return &Expr{Kind: KindNot, Children: []*Expr{child}} }
