package main

import (
	"fmt"
)

type Parser struct {
	tokens   []Token
	pos      int
	filename string
}

func NewParser(tokens []Token, filename string) *Parser {
	return &Parser{tokens: tokens, filename: filename}
}

func (p *Parser) current() Token { return p.tokens[p.pos] }

func (p *Parser) advance() Token {
	t := p.tokens[p.pos]
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
	return t
}

func (p *Parser) expect(type_ string) Token {
	if p.current().Type != type_ {
		t := p.current()
		panic(syntaxErr("%s:%d:%d: Expected %s, got %s", p.filename, t.Line, t.Col, type_, t.Type))
	}
	return p.advance()
}

func (p *Parser) match(types ...string) bool {
	for _, t := range types {
		if p.current().Type == t {
			return true
		}
	}
	return false
}

func (p *Parser) curOpIn(vals ...string) bool {
	if p.current().Type != "OP" {
		return false
	}
	v, _ := p.current().Value.(string)
	for _, x := range vals {
		if v == x {
			return true
		}
	}
	return false
}

func (p *Parser) curOp() string {
	v, _ := p.current().Value.(string)
	return v
}

func (p *Parser) skipNewlines() {
	for p.match("NEWLINE", "SEMI") {
		p.advance()
	}
}

func (p *Parser) Parse() *Program {
	var stmts []Node
	p.skipNewlines()
	for !p.match("EOF") {
		stmt := p.statement()
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
		p.skipNewlines()
	}
	return &Program{Stmts: stmts}
}

func (p *Parser) statement() Node {
	p.skipNewlines()
	if p.match("EOF") {
		return nil
	}
	switch {
	case p.match("PRINT", "SAY"):
		return p.printStmt()
	case p.match("IF"):
		return p.ifStmt()
	case p.match("WHILE"):
		return p.whileStmt()
	case p.match("FOR"):
		return p.forStmt()
	case p.match("DEF"):
		return p.defStmt(false)
	case p.match("CLASS"):
		return p.classStmt()
	case p.match("RETURN"):
		return p.returnStmt()
	case p.match("BREAK"):
		p.advance()
		return &BreakStmt{}
	case p.match("CONTINUE"):
		p.advance()
		return &ContinueStmt{}
	case p.match("PASS"):
		p.advance()
		return &PassStmt{}
	case p.match("RAISE"):
		return p.raiseStmt()
	case p.match("TRY"):
		return p.tryStmt()
	case p.match("IMPORT"):
		return p.importStmt()
	case p.match("FROM"):
		return p.fromImportStmt()
	case p.match("FORTA"):
		return p.fortaStmt()
	case p.match("NESEJO"):
		return p.nesejoStmt()
	case p.match("SHFAQ"):
		return p.shfaqStmt()
	case p.match("REMEMBER"):
		return p.rememberStmt()
	case p.match("LBRACE"):
		return p.block()
	}
	expr := p.expression()
	if p.current().Type == "OP" && p.curOp() == "=" {
		p.advance()
		return &Assign{Target: expr, V: p.expression()}
	}
	if p.current().Type == "OP" && p.curOpIn("+=", "-=", "*=", "/=", "%=", "**=", "//=") {
		op := p.advance().Value.(string)
		return &AugAssign{Target: expr, Op: op, V: p.expression()}
	}
	return &ExprStmt{E: expr}
}

func (p *Parser) printStmt() Node {
	p.advance()
	exprs := []Node{p.expression()}
	for p.match("COMMA") {
		p.advance()
		exprs = append(exprs, p.expression())
	}
	return &PrintStmt{Exprs: exprs}
}

func (p *Parser) fortaStmt() Node {
	p.advance()
	prob := p.expression()
	return &ChanceStmt{Prob: prob, Body: p.blockOrStmt()}
}

func (p *Parser) nesejoStmt() Node {
	p.advance()
	cond := p.expression()
	return &UnlessStmt{Cond: cond, Body: p.blockOrStmt()}
}

func (p *Parser) shfaqStmt() Node {
	p.advance()
	return &EchoStmt{E: p.expression()}
}

func (p *Parser) rememberStmt() Node {
	p.advance()
	name := p.expect("ID").Value.(string)
	p.skipNewlines()
	if p.current().Type == "OP" && p.curOp() == "=" {
		p.advance()
	}
	return &RememberStmt{Name: name, V: p.expression()}
}

func (p *Parser) ifStmt() Node {
	p.advance()
	cond := p.expression()
	then := p.blockOrStmt()
	var elifs []ElifPart
	var elseBody Node
	p.skipNewlines()
	for p.match("ELIF") {
		p.advance()
		c := p.expression()
		b := p.blockOrStmt()
		elifs = append(elifs, ElifPart{Cond: c, Body: b})
		p.skipNewlines()
	}
	if p.match("ELSE") {
		p.advance()
		elseBody = p.blockOrStmt()
	}
	return &IfStmt{Cond: cond, Then: then, Elifs: elifs, Else: elseBody}
}

func (p *Parser) whileStmt() Node {
	p.advance()
	cond := p.expression()
	return &WhileStmt{Cond: cond, Body: p.blockOrStmt()}
}

func (p *Parser) forStmt() Node {
	p.advance()
	if !p.match("ID") {
		panic(syntaxErr("for loop variable must be a simple name"))
	}
	targetName := p.advance().Value.(string)
	target := &Ident{Name: targetName}
	p.skipNewlines()
	if p.match("IN") {
		p.advance()
		p.skipNewlines()
		iter := p.expression()
		return &ForIn{Target: target, Iterable: iter, Body: p.blockOrStmt()}
	}
	if p.current().Type == "OP" && p.curOp() == "=" {
		p.advance()
		start := p.expression()
		p.expect("TO")
		end := p.expression()
		var step Node
		if p.match("COMMA") {
			p.advance()
			step = p.expression()
		}
		return &ForRange{Var: targetName, Start: start, End: end, Step: step, Body: p.blockOrStmt()}
	}
	panic(syntaxErr("Invalid for"))
}

func (p *Parser) defStmt(isMethod bool) Node {
	p.advance()
	name := p.expect("ID").Value.(string)
	p.expect("LPAREN")
	params := p.parseParams()
	p.expect("RPAREN")
	return &FuncDef{Name: name, Params: params, Body: p.blockOrStmt(), IsMethod: isMethod}
}

func (p *Parser) classStmt() Node {
	p.advance()
	name := p.expect("ID").Value.(string)
	var bases []Node
	if p.match("LPAREN") {
		p.advance()
		if !p.match("RPAREN") {
			bases = append(bases, p.expression())
			for p.match("COMMA") {
				p.advance()
				bases = append(bases, p.expression())
			}
		}
		p.expect("RPAREN")
	}
	return &ClassDef{Name: name, Bases: bases, Body: p.block()}
}

func (p *Parser) returnStmt() Node {
	p.advance()
	if p.match("NEWLINE", "SEMI", "RBRACE", "EOF") {
		return &ReturnStmt{}
	}
	return &ReturnStmt{V: p.expression()}
}

func (p *Parser) raiseStmt() Node {
	p.advance()
	if p.match("NEWLINE", "SEMI", "RBRACE", "EOF") {
		return &RaiseStmt{}
	}
	return &RaiseStmt{E: p.expression()}
}

func (p *Parser) tryStmt() Node {
	p.advance()
	body := p.blockOrStmt()
	var handlers []TryHandler
	var finallyBody Node
	p.skipNewlines()
	for p.match("CATCH", "EXCEPT") {
		p.advance()
		name := ""
		if !p.match("LBRACE") && !p.match("NEWLINE") {
			first := p.expression()
			if p.match("AS") {
				p.advance()
				name = p.expect("ID").Value.(string)
			} else if id, ok := first.(*Ident); ok {
				name = id.Name
			}
		}
		handlers = append(handlers, TryHandler{Name: name, Body: p.blockOrStmt()})
		p.skipNewlines()
	}
	if p.match("FINALLY") {
		p.advance()
		finallyBody = p.blockOrStmt()
	}
	return &TryStmt{Body: body, Handlers: handlers, Finally: finallyBody}
}

func (p *Parser) importStmt() Node {
	p.advance()
	module := p.expect("ID").Value.(string)
	for p.match("DOT") {
		p.advance()
		module += "." + p.expect("ID").Value.(string)
	}
	alias := ""
	if p.match("AS") {
		p.advance()
		alias = p.expect("ID").Value.(string)
	}
	return &ImportStmt{Module: module, Alias: alias}
}

func (p *Parser) fromImportStmt() Node {
	p.advance()
	module := p.expect("ID").Value.(string)
	for p.match("DOT") {
		p.advance()
		module += "." + p.expect("ID").Value.(string)
	}
	p.expect("IMPORT")
	var names [][2]string
	for {
		name := p.expect("ID").Value.(string)
		alias := ""
		if p.match("AS") {
			p.advance()
			alias = p.expect("ID").Value.(string)
		}
		names = append(names, [2]string{name, alias})
		if !p.match("COMMA") {
			break
		}
		p.advance()
	}
	return &FromImportStmt{Module: module, Names: names}
}

func (p *Parser) blockOrStmt() Node {
	p.skipNewlines()
	if p.match("LBRACE") {
		return p.block()
	}
	stmt := p.statement()
	if stmt == nil {
		return &Block{}
	}
	return &Block{Stmts: []Node{stmt}}
}

func (p *Parser) block() *Block {
	p.expect("LBRACE")
	var stmts []Node
	p.skipNewlines()
	for !p.match("RBRACE", "EOF") {
		stmt := p.statement()
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
		p.skipNewlines()
	}
	p.expect("RBRACE")
	return &Block{Stmts: stmts}
}

func (p *Parser) parseParams() []string {
	var params []string
	if !p.match("RPAREN") {
		params = append(params, p.expect("ID").Value.(string))
		for p.match("COMMA") {
			p.advance()
			params = append(params, p.expect("ID").Value.(string))
		}
	}
	return params
}

func (p *Parser) expression() Node { return p.logicOr() }

func (p *Parser) logicOr() Node {
	left := p.logicAnd()
	for p.match("OR") {
		op := p.advance().Value.(string)
		left = &Binary{Op: op, L: left, R: p.logicAnd()}
	}
	return left
}

func (p *Parser) logicAnd() Node {
	left := p.equality()
	for p.match("AND") {
		op := p.advance().Value.(string)
		left = &Binary{Op: op, L: left, R: p.equality()}
	}
	return left
}

func (p *Parser) equality() Node {
	left := p.comparison()
	for (p.current().Type == "OP" && p.curOpIn("==", "!=")) || p.match("IS") {
		op := p.advance().Value.(string)
		left = &Binary{Op: op, L: left, R: p.comparison()}
	}
	return left
}

func (p *Parser) comparison() Node {
	left := p.term()
	for (p.current().Type == "OP" && p.curOpIn("<", ">", "<=", ">=")) || p.match("IN") {
		op := p.advance().Value.(string)
		left = &Binary{Op: op, L: left, R: p.term()}
	}
	return left
}

func (p *Parser) term() Node {
	left := p.factor()
	for p.current().Type == "OP" && p.curOpIn("+", "-") {
		op := p.advance().Value.(string)
		left = &Binary{Op: op, L: left, R: p.factor()}
	}
	return left
}

func (p *Parser) factor() Node {
	left := p.unary()
	for p.current().Type == "OP" && p.curOpIn("*", "/", "%", "//") {
		op := p.advance().Value.(string)
		left = &Binary{Op: op, L: left, R: p.unary()}
	}
	return left
}

func (p *Parser) unary() Node {
	if p.match("NOT") || (p.current().Type == "OP" && p.curOpIn("-", "!", "+", "~")) {
		op := p.advance().Value.(string)
		return &Unary{Op: op, E: p.unary()}
	}
	return p.power()
}

func (p *Parser) power() Node {
	left := p.postfix()
	if p.current().Type == "OP" && p.curOp() == "**" {
		op := p.advance().Value.(string)
		return &Binary{Op: op, L: left, R: p.unary()}
	}
	return left
}

func (p *Parser) postfix() Node {
	expr := p.primary()
	for {
		switch {
		case p.match("LPAREN"):
			p.advance()
			var args []Node
			kwargs := map[string]Node{}
			var order []string
			if !p.match("RPAREN") {
				first := p.expression()
				if p.current().Type == "OP" && p.curOp() == "=" {
					if id, ok := first.(*Ident); ok {
						p.advance()
						kwargs[id.Name] = p.expression()
						order = append(order, id.Name)
					} else {
						args = append(args, first)
					}
				} else {
					args = append(args, first)
				}
				for p.match("COMMA") {
					p.advance()
					arg := p.expression()
					if p.current().Type == "OP" && p.curOp() == "=" {
						if id, ok := arg.(*Ident); ok {
							p.advance()
							kwargs[id.Name] = p.expression()
							order = append(order, id.Name)
							continue
						}
					}
					args = append(args, arg)
				}
			}
			p.expect("RPAREN")
			expr = &Call{Fn: expr, Args: args, Kwargs: kwargs, KwargOrder: order}
		case p.match("LBRACK"):
			p.advance()
			idx := p.expression()
			p.expect("RBRACK")
			expr = &Index{Obj: expr, Idx: idx}
		case p.match("DOT"):
			p.advance()
			expr = &Attr{Obj: expr, Name: p.expect("ID").Value.(string)}
		case p.current().Type == "OP" && p.curOp() == "~":
			p.advance()
			depth := 1
			if p.match("NUMBER") {
				tok := p.advance()
				switch v := tok.Value.(type) {
				case int64:
					depth = int(v)
				case float64:
					depth = int(v)
				}
			}
			id, ok := expr.(*Ident)
			if !ok {
				panic(syntaxErr("~ only valid on variables"))
			}
			expr = &PastExpr{Name: id.Name, Depth: depth}
		default:
			return expr
		}
	}
}

func (p *Parser) primary() Node {
	tok := p.current()
	switch {
	case p.match("NUMBER"):
		p.advance()
		return &NumLit{V: tok.Value}
	case p.match("STRING"):
		p.advance()
		return &StrLit{V: tok.Value.(string)}
	case p.match("TRUE"):
		p.advance()
		return &BoolLit{V: true}
	case p.match("FALSE"):
		p.advance()
		return &BoolLit{V: false}
	case p.match("NULL"):
		p.advance()
		return &NullLit{}
	case p.match("ID"):
		p.advance()
		return &Ident{Name: tok.Value.(string)}
	case p.match("LPAREN"):
		p.advance()
		e := p.expression()
		p.expect("RPAREN")
		return e
	case p.match("LBRACK"):
		p.advance()
		var elems []Node
		if !p.match("RBRACK") {
			elems = append(elems, p.expression())
			for p.match("COMMA") {
				p.advance()
				if p.match("RBRACK") {
					break
				}
				elems = append(elems, p.expression())
			}
		}
		p.expect("RBRACK")
		return &ListLit{Elems: elems}
	case p.match("LBRACE"):
		p.advance()
		var pairs [][2]Node
		if !p.match("RBRACE") {
			k := p.expression()
			p.expect("COLON")
			v := p.expression()
			pairs = append(pairs, [2]Node{k, v})
			for p.match("COMMA") {
				p.advance()
				if p.match("RBRACE") {
					break
				}
				k := p.expression()
				p.expect("COLON")
				v := p.expression()
				pairs = append(pairs, [2]Node{k, v})
			}
		}
		p.expect("RBRACE")
		return &DictLit{Pairs: pairs}
	case p.match("LAMBDA", "FN"):
		p.advance()
		var params []string
		if p.match("LPAREN") {
			p.advance()
			params = p.parseParams()
			p.expect("RPAREN")
		} else if p.match("ID") {
			params = append(params, p.advance().Value.(string))
		}
		if p.match("COLON") || (p.current().Type == "OP" && p.curOp() == ":") {
			p.advance()
			return &LambdaExpr{Params: params, Body: p.expression()}
		}
		return &LambdaExpr{Params: params, Body: p.blockOrStmt()}
	}
	t := p.current()
	panic(syntaxErr("%s:%d:%d: Unexpected %s", p.filename, t.Line, t.Col, t.Type))
}

var _ = fmt.Sprintf
