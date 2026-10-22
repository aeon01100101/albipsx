package main

type Node interface{ node() }

type NumLit struct{ V Value }
type StrLit struct{ V string }
type BoolLit struct{ V bool }
type NullLit struct{}
type Ident struct{ Name string }
type Binary struct {
	Op   string
	L, R Node
}
type Unary struct {
	Op string
	E  Node
}
type ListLit struct{ Elems []Node }
type DictLit struct{ Pairs [][2]Node }
type Index struct{ Obj, Idx Node }
type Attr struct {
	Obj  Node
	Name string
}
type Call struct {
	Fn         Node
	Args       []Node
	Kwargs     map[string]Node
	KwargOrder []string
}
type Assign struct{ Target, V Node }
type AugAssign struct {
	Target Node
	Op     string
	V      Node
}
type PrintStmt struct{ Exprs []Node }
type IfStmt struct {
	Cond  Node
	Then  Node
	Elifs []ElifPart
	Else  Node
}
type ElifPart struct {
	Cond Node
	Body Node
}
type WhileStmt struct{ Cond, Body Node }
type ForIn struct {
	Target   Node
	Iterable Node
	Body     Node
}
type ForRange struct {
	Var   string
	Start Node
	End   Node
	Step  Node
	Body  Node
}
type FuncDef struct {
	Name     string
	Params   []string
	Body     Node
	IsMethod bool
}
type LambdaExpr struct {
	Params []string
	Body   Node
}
type ClassDef struct {
	Name  string
	Bases []Node
	Body  *Block
}
type ReturnStmt struct{ V Node }
type BreakStmt struct{}
type ContinueStmt struct{}
type PassStmt struct{}
type RaiseStmt struct{ E Node }
type TryHandler struct {
	Name string
	Body Node
}
type TryStmt struct {
	Body     Node
	Handlers []TryHandler
	Finally  Node
}
type ImportStmt struct {
	Module string
	Alias  string
}
type FromImportStmt struct {
	Module string
	Names  [][2]string // (name, alias)
}
type Block struct{ Stmts []Node }
type Program struct{ Stmts []Node }
type ExprStmt struct{ E Node }
type ChanceStmt struct {
	Prob Node
	Body Node
}
type UnlessStmt struct{ Cond, Body Node }
type EchoStmt struct{ E Node }
type PastExpr struct {
	Name  string
	Depth int
}
type RememberStmt struct {
	Name string
	V    Node
}

func (*NumLit) node()         {}
func (*StrLit) node()         {}
func (*BoolLit) node()        {}
func (*NullLit) node()        {}
func (*Ident) node()          {}
func (*Binary) node()         {}
func (*Unary) node()          {}
func (*ListLit) node()        {}
func (*DictLit) node()        {}
func (*Index) node()          {}
func (*Attr) node()           {}
func (*Call) node()           {}
func (*Assign) node()         {}
func (*AugAssign) node()      {}
func (*PrintStmt) node()      {}
func (*IfStmt) node()         {}
func (*WhileStmt) node()      {}
func (*ForIn) node()          {}
func (*ForRange) node()       {}
func (*FuncDef) node()        {}
func (*LambdaExpr) node()     {}
func (*ClassDef) node()       {}
func (*ReturnStmt) node()     {}
func (*BreakStmt) node()      {}
func (*ContinueStmt) node()   {}
func (*PassStmt) node()       {}
func (*RaiseStmt) node()      {}
func (*TryStmt) node()        {}
func (*ImportStmt) node()     {}
func (*FromImportStmt) node() {}
func (*Block) node()          {}
func (*Program) node()        {}
func (*ExprStmt) node()       {}
func (*ChanceStmt) node()     {}
func (*UnlessStmt) node()     {}
func (*EchoStmt) node()       {}
func (*PastExpr) node()       {}
func (*RememberStmt) node()   {}
