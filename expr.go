package audience

import "fmt"

// 单条表达式的规模上限。
const (
	MaxPositions = 256 // 标签与运算符出现位置总数上限
	MaxDepth     = 32  // 嵌套层数上限（根为第 1 层）
)

// Op 是表达式节点的运算符种类。
type Op uint8

const (
	OpTag Op = iota // 标签叶子节点
	OpAnd           // 合取，至少两个操作数
	OpOr            // 析取，至少两个操作数
	OpNot           // 取反，恰好一个操作数
)

// ExprNode 是调用方构造的表达式树节点。
// OpTag 时 Tag 为正整数标签 ID，Args 为空；
// OpAnd/OpOr 时 Args 至少两个；OpNot 时 Args 恰好一个。
type ExprNode struct {
	Op   Op
	Tag  uint32
	Args []*ExprNode
}

// Tag 构造一个标签叶子节点。
func Tag(id uint32) *ExprNode { return &ExprNode{Op: OpTag, Tag: id} }

// And 构造合取节点。
func And(args ...*ExprNode) *ExprNode { return &ExprNode{Op: OpAnd, Args: args} }

// Or 构造析取节点。
func Or(args ...*ExprNode) *ExprNode { return &ExprNode{Op: OpOr, Args: args} }

// Not 构造取反节点。
func Not(arg *ExprNode) *ExprNode { return &ExprNode{Op: OpNot, Args: []*ExprNode{arg}} }

// validate 校验表达式合法性：结构、操作数数量、标签 ID、规模与嵌套上限。
// 输入保证合法，这里仅作防御性检查。
func validate(root *ExprNode) error {
	if root == nil {
		return fmt.Errorf("audience: 表达式为空")
	}
	positions := 0
	return validateNode(root, 1, &positions)
}

func validateNode(n *ExprNode, depth int, positions *int) error {
	if n == nil {
		return fmt.Errorf("audience: 表达式含空节点")
	}
	if depth > MaxDepth {
		return fmt.Errorf("audience: 嵌套超过 %d 层", MaxDepth)
	}
	*positions++
	if *positions > MaxPositions {
		return fmt.Errorf("audience: 出现位置超过 %d 个", MaxPositions)
	}
	switch n.Op {
	case OpTag:
		if n.Tag == 0 {
			return fmt.Errorf("audience: 标签 ID 必须为正整数")
		}
		if len(n.Args) != 0 {
			return fmt.Errorf("audience: 标签节点不应有操作数")
		}
	case OpAnd, OpOr:
		if len(n.Args) < 2 {
			return fmt.Errorf("audience: AND/OR 至少需要两个操作数")
		}
	case OpNot:
		if len(n.Args) != 1 {
			return fmt.Errorf("audience: NOT 恰好一个操作数")
		}
	default:
		return fmt.Errorf("audience: 未知运算符 %d", n.Op)
	}
	for _, arg := range n.Args {
		if err := validateNode(arg, depth+1, positions); err != nil {
			return err
		}
	}
	return nil
}
