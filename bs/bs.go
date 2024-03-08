package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"text/scanner"
)

var (
	errVarNotDefined = errors.New("variable not defined")
)

type tokenT int

const (
	tokNil tokenT = iota
	tokEOL
	tokId
	tokInt
	tokComma
	tokColon
	tokLBracket
	tokRBracket
)

type token struct {
	t tokenT
	v string
}
type tokens []token

func (t tokens) consume() tokens {
	if len(t) == 0 {
		return nil
	}
	return t[1:]
}

func (t tokens) next() *token {
	if len(t) == 0 {
		return nil
	}
	return &t[0]
}

func (t tokens) peek() *token {
	if len(t) < 2 {
		return nil
	}
	return &t[1]
}

func lex(s *scanner.Scanner) []token {
	tokens := make([]token, 0)
	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
		switch tok {
		case '\n':
			tokens = append(tokens, token{tokEOL, s.TokenText()})
		case scanner.Ident:
			tokens = append(tokens, token{tokId, s.TokenText()})
		case scanner.Int:
			tokens = append(tokens, token{tokInt, s.TokenText()})
		case ',':
			tokens = append(tokens, token{tokComma, s.TokenText()})
		case ':':
			tokens = append(tokens, token{tokColon, s.TokenText()})
		case '[':
			tokens = append(tokens, token{tokLBracket, s.TokenText()})
		case ']':
			tokens = append(tokens, token{tokRBracket, s.TokenText()})
		default:
			fmt.Printf("%s: %s\n", scanner.TokenString(tok), s.TokenText())
		}
	}
	return tokens
}

type expr interface {
	int(lookup func(string) expr) (int, error)
}

type exprId string

func (id exprId) int(lookup func(string) expr) (int, error) {
	e := lookup(string(id))
	if e == nil {
		return 0, fmt.Errorf("%q %w", string(id), errVarNotDefined)
	}
	return e.int(lookup)
}

type exprInt int

func (e exprInt) int(lookup func(string) expr) (int, error) {
	return int(e), nil
}

type exprAddr struct{ expr } // exprAddr represents an expression which evaluates to the address of memory (a cell). E.x. [5]

// int returns an error for exprAddr to prevent incorrect behavior. To evaluate the inside
// expr, call the int() method on the exprAddr.expr value once the type has been checked.
func (e exprAddr) int(lookup func(string) expr) (int, error) {
	panic("cannot call int() on exprAddr; not an int itself")
}

type stmt interface{}
type stmtInstr struct {
	name     string
	dst, src expr
}
type stmtLabel string

type ast struct {
	stmts []stmt
}

func parseExprId(toks tokens) (tokens, exprId) {
	if toks.next().t != tokId {
		panic("expected exprId")
	}
	val := toks.next().v
	toks = toks.consume()
	return toks, exprId(val)
}

func parseExprInt(toks tokens) (tokens, exprInt) {
	if toks.next().t != tokInt {
		panic("expected exprInt")
	}
	v, err := strconv.Atoi(toks.next().v)
	if err != nil {
		panic(err)
	}
	toks = toks.consume()
	return toks, exprInt(v)
}

func parseExprAddr(toks tokens) (tokens, exprAddr) {
	if toks.next().t != tokLBracket {
		panic("exprAddr must start with left bracket ([)")
	}
	toks = toks.consume()

	toks, expr := parseExpr(toks)

	if toks.next().t != tokRBracket {
		panic("exprAddr must have a closing right bracket (])")
	}
	toks = toks.consume()

	return toks, exprAddr{expr}
}

// expr: exprAddr | exprInt
func parseExpr(toks tokens) (tokens, expr) {
	if toks.next().t == tokId {
		return parseExprId(toks)
	} else if toks.next().t == tokLBracket {
		return parseExprAddr(toks)
	} else {
		return parseExprInt(toks)
	}
}

func parseStmtInstr(toks tokens) (tokens, *stmtInstr) {
	instr := new(stmtInstr)
	if toks.next().t != tokId {
		panic("expected tokId")
	}
	instr.name = strings.ToLower(toks.next().v)
	toks = toks.consume()

	// Some instructions may have no arguments
	if tok := toks.next(); tok == nil || tok.t == tokEOL {
		return toks, instr
	}

	// Parse first argument to instruction
	toks, instr.dst = parseExpr(toks)

	// Some instructions may have only one argument
	if tok := toks.next(); tok == nil || tok.t == tokEOL {
		return toks, instr
	}

	// Expect a comma separating expressions
	if toks.next().t != tokComma {
		panic("expected comma separator")
	}
	toks = toks.consume()

	// Parse second argument
	toks, instr.src = parseExpr(toks)

	return toks, instr
}

func parseStmt(toks tokens) (tokens, stmt) {
	if toks.next().t != tokId {
		panic("expected tokId")
	}

	// Parse stmtLabel
	if toks.peek().t == tokColon { // Ex. "myLabel:"
		label := toks.next().v
		toks = toks.consume() // Consume myLabel
		toks = toks.consume() // Consume :
		return toks, stmtLabel(label)
	}

	return parseStmtInstr(toks)
}

func parse(toks tokens) *ast {
	ast := &ast{}
	for toks.next() != nil {
		// Skip whitespace
		if toks.next().t == tokEOL {
			toks = toks.consume()
			continue
		}

		var stmt stmt
		toks, stmt = parseStmt(toks)
		ast.stmts = append(ast.stmts, stmt)
	}
	return ast
}

type gen struct {
	sb  strings.Builder
	ptr int

	label      string          // Label name assigned to the next generated instruction.
	labelTable map[string]expr // LabelTable sounds cool.
	loopStarts []int
}

func (g *gen) point(at int) {
	diff := at - g.ptr
	if diff < 0 {
		for range -diff {
			g.sb.WriteRune('<')
		}
	} else {
		for range diff {
			g.sb.WriteRune('>')
		}
	}
	g.ptr = at
}

func (g *gen) pushLoopStart(ptr int) {
	g.loopStarts = append(g.loopStarts, ptr)
}

// popLoopStart returns start index of loopStart().
func (g *gen) popLoopStart() int {
	start := g.loopStarts[len(g.loopStarts)-1]
	g.loopStarts = g.loopStarts[:len(g.loopStarts)-1] // Pop value
	return start
}

func (g *gen) loopStart() {
	// store current ptr value
	// then write a loop start
	g.pushLoopStart(g.ptr)
	g.sb.WriteRune('[')
}

// loopEnd calls g.point() with popLoopStart()'s return value and writes the closing bracket ']'.
func (g *gen) loopEnd() {
	// go back to stored ptr value of matching loopstart (pop loopstart)
	// write loop end
	g.point(g.popLoopStart())
	g.sb.WriteRune(']')
}

func (g *gen) lookup(id string) expr {
	if e, ok := g.labelTable[id]; ok {
		return e
	}
	return nil
}

func (g *gen) instr(instr *stmtInstr) {
	switch instr.name {
	case "inc":
		times := 1
		if addr, ok := instr.dst.(exprAddr); ok { // inc [1], 5
			v, err := addr.expr.int(g.lookup)
			if err != nil {
				panic(err)
			}
			g.point(v)
		} else {
			panic("first argument must be an address")
		}
		if instr.src != nil {
			v, err := instr.src.int(g.lookup)
			if err != nil {
				panic("second argument must be an integer: " + err.Error())
			}
			times = v
		}
		for range times {
			g.sb.WriteRune('+')
		}
	case "dec":
		times := 1
		if addr, ok := instr.dst.(exprAddr); ok { // dec [1], 5
			v, err := addr.expr.int(g.lookup)
			if err != nil {
				panic(err)
			}
			g.point(v)
		} else {
			panic("first argument must be an address")
		}
		if instr.src != nil {
			v, err := instr.src.int(g.lookup)
			if err != nil {
				panic("second argument must be an integer: " + err.Error())
			}
			times = v
		}
		for range times {
			g.sb.WriteRune('-')
		}
	case "while":
		if addr, ok := instr.dst.(exprAddr); ok { // while [1]
			v, err := addr.expr.int(g.lookup)
			if err != nil {
				panic(err)
			}
			g.point(v)
		} else {
			panic("first argument must be an address")
		}
		g.loopStart()
	case "endwhile":
		if addr, ok := instr.dst.(exprAddr); ok { // endwhile [1]
			v, err := addr.expr.int(g.lookup)
			if err != nil {
				panic(err)
			}

			g.popLoopStart() // Pop but discard the loopStart index.
			g.point(v)
			g.sb.WriteRune(']')
		} else {
			g.loopEnd()
		}
	case "call":
		if addr, ok := instr.dst.(exprAddr); ok { // call [1]
			v, err := addr.expr.int(g.lookup)
			if err != nil {
				panic(err)
			}
			g.point(v)
		} else {
			panic("first argument must be an address")
		}
		g.sb.WriteRune('.')
	case "read":
		if addr, ok := instr.dst.(exprAddr); ok { // read [1]
			v, err := addr.expr.int(g.lookup)
			if err != nil {
				panic(err)
			}
			g.point(v)
		} else {
			panic("first argument must be an address")
		}
		g.sb.WriteRune(',')
	case "clear":
		if addr, ok := instr.dst.(exprAddr); ok { // clear [1]
			v, err := addr.expr.int(g.lookup)
			if err != nil {
				panic(err)
			}
			g.point(v)
		} else {
			panic("first argument must be an address")
		}
		g.sb.WriteString("[-]")
	case "if": // if [0] [1] ([0] is the condition; junks both cells)
		var condPtr int
		var junkPtr int

		if addr, ok := instr.dst.(exprAddr); ok {
			v, err := addr.expr.int(g.lookup)
			if err != nil {
				panic(err)
			}
			condPtr = v
		} else {
			panic("first argument must be the address of the conditional cell that will be checked and cleared")
		}

		if addr, ok := instr.src.(exprAddr); ok {
			v, err := addr.expr.int(g.lookup)
			if err != nil {
				panic(err)
			}
			junkPtr = v // preferably close to the first address...
		} else {
			panic("second argument must be the address of a cell that can be junked in the process")
		}

		g.point(junkPtr)         // Go to the junkPtr
		g.sb.WriteString("[-]+") // Set junkPtr to 1

		// Save the addresses to our stack and start the conditional check
		g.point(condPtr)
		g.pushLoopStart(condPtr) // New stack is [..., condPtr, junkPtr]
		g.pushLoopStart(junkPtr)
		g.sb.WriteRune('[')

		// This code applies when the condition is true...
		g.sb.WriteString("[-]") // Clear condPtr to break loop
		g.point(junkPtr)
		g.sb.WriteString("[-]") // Clear junkPtr to prevent else statement
	case "else":
		junkPtr := g.popLoopStart()
		condPtr := g.popLoopStart()
		g.point(condPtr)    // Go back to the condPtr to exit the loop
		g.sb.WriteRune(']') // Exit loop

		g.point(junkPtr)
		g.sb.WriteRune('[') // If the condition was false, the junkPtr will activate the loop

		g.pushLoopStart(condPtr) // Still have to push our addresses for the endif
		g.pushLoopStart(junkPtr)
	case "endif":
		_ = g.popLoopStart()        // Pop junkPtr
		condPtr := g.popLoopStart() // Pop condPtr
		g.point(condPtr)            // Go back to condPtr (notice how 'else' also does this at the end)
		g.sb.WriteRune(']')
	case "const":
		if instr.dst == nil || instr.src != nil {
			panic("const must have one value")
		}
		value := instr.dst
		if g.label == "" {
			panic("const must have a label before it")
		}
		if _, exists := g.labelTable[g.label]; exists {
			panic("a const cannot be shadowed by another const with the same name")
		}
		g.labelTable[g.label] = value
	default:
		panic("not a valid instruction name: " + instr.name)
	}

	g.label = "" // Labels only apply to the first instruction after them.
}

func generate(ast *ast) string {
	gen := new(gen)
	gen.labelTable = make(map[string]expr)

	for _, stmt := range ast.stmts {
		switch s := stmt.(type) {
		case *stmtInstr:
			gen.instr(s)
		case stmtLabel:
			gen.label = string(s)
		}
	}

	return gen.sb.String()
}

func main() {
	flagTokens := flag.Bool("tokens", false, "print tokens")
	flagAst := flag.Bool("ast", false, "print ast")
	flagOutput := flag.String("o", "", "output file")

	flag.Parse()

	if len(flag.Args()) < 1 {
		panic("no input file")
	}

	inputName := flag.Arg(0)
	input, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		panic(err)
	}

	s := &scanner.Scanner{}
	s.Init(bytes.NewReader(input))
	s.Whitespace ^= 1 << '\n' // Don't skip EOL
	s.Filename = os.Args[1]

	tokens := lex(s)

	if *flagTokens {
		fmt.Println("Tokens:")
		for _, t := range tokens {
			fmt.Printf("%q\n", t.v)
		}
	}

	ast := parse(tokens)

	if *flagAst {
		fmt.Println("\nParsed instructions:")
		for i, in := range ast.stmts {
			fmt.Printf("%3d: %#v\n", i+1, in)
		}
		fmt.Println()
	}

	generated := generate(ast)

	var outputName string
	if *flagOutput != "" {
		outputName = *flagOutput
	} else {
		outputName = strings.TrimSuffix(inputName, path.Ext(inputName))
		outputName += ".bf"
	}

	os.WriteFile(outputName, []byte(generated), 0644)
}
