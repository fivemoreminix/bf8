package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"text/scanner"
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

type expr interface{}
type exprInt int             // exprInt is an expression which evaluates to a single integer.
type exprAddr struct{ expr } // exprAddr represents an expression which evaluates to the address of memory (a cell). E.x. [5]

type instr struct {
	name     string
	dst, src expr
}

type ast struct {
	instr []*instr
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
	if toks.next().t == tokLBracket {
		return parseExprAddr(toks)
	} else {
		return parseExprInt(toks)
	}
}

func parseInstr(toks tokens) (tokens, *instr) {
	instr := new(instr)
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

func parse(toks tokens) *ast {
	ast := &ast{}
	for toks.next() != nil {
		// Skip whitespace
		if toks.next().t == tokEOL {
			toks = toks.consume()
			continue
		}

		var instr *instr
		toks, instr = parseInstr(toks)
		ast.instr = append(ast.instr, instr)
	}
	return ast
}

type gen struct {
	sb  strings.Builder
	ptr int

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

func generate(ast *ast) string {
	gen := new(gen)

	for _, instr := range ast.instr {
		switch instr.name {
		case "inc":
			times := 1
			if addr, ok := instr.dst.(exprAddr); ok { // inc [1], 5
				gen.point(int(addr.expr.(exprInt)))
			} else {
				panic("first argument must be an address")
			}
			if instr.src != nil {
				if v, ok := instr.src.(exprInt); ok {
					times = int(v)
				} else {
					panic("second argument must be an integer")
				}
			}
			for range times {
				gen.sb.WriteRune('+')
			}
		case "dec":
			times := 1
			if addr, ok := instr.dst.(exprAddr); ok { // dec [1], 5
				gen.point(int(addr.expr.(exprInt)))
			} else {
				panic("first argument must be an address")
			}
			if instr.src != nil {
				if v, ok := instr.src.(exprInt); ok {
					times = int(v)
				} else {
					panic("second argument must be an integer")
				}
			}
			for range times {
				gen.sb.WriteRune('-')
			}
		case "while":
			if addr, ok := instr.dst.(exprAddr); ok { // while [1]
				gen.point(int(addr.expr.(exprInt)))
			} else {
				panic("first argument must be an address")
			}
			gen.loopStart()
		case "endwhile":
			if addr, ok := instr.dst.(exprAddr); ok { // endwhile [1]
				gen.popLoopStart() // Pop but discard the loopStart index.
				gen.point(int(addr.expr.(exprInt)))
				gen.sb.WriteRune(']')
			} else {
				gen.loopEnd()
			}
		case "call":
			if addr, ok := instr.dst.(exprAddr); ok { // call [1]
				gen.point(int(addr.expr.(exprInt)))
			} else {
				panic("first argument must be an address")
			}
			gen.sb.WriteRune('.')
		case "read":
			if addr, ok := instr.dst.(exprAddr); ok { // read [1]
				gen.point(int(addr.expr.(exprInt)))
			} else {
				panic("first argument must be an address")
			}
			gen.sb.WriteRune(',')
		case "clear":
			if addr, ok := instr.dst.(exprAddr); ok { // clear [1]
				gen.point(int(addr.expr.(exprInt)))
			} else {
				panic("first argument must be an address")
			}
			gen.sb.WriteString("[-]")
		case "if": // if [0] [1] ([0] is the condition; junks both cells)
			var condPtr int
			var junkPtr int

			if addr, ok := instr.dst.(exprAddr); ok {
				condPtr = int(addr.expr.(exprInt))
			} else {
				panic("first argument must be the address of the conditional cell that will be checked and cleared")
			}

			if addr, ok := instr.src.(exprAddr); ok {
				junkPtr = int(addr.expr.(exprInt)) // preferably close to the first address...
			} else {
				panic("second argument must be the address of a cell that can be junked in the process")
			}

			gen.point(junkPtr)         // Go to the junkPtr
			gen.sb.WriteString("[-]+") // Set junkPtr to 1

			// Save the addresses to our stack and start the conditional check
			gen.point(condPtr)
			gen.pushLoopStart(condPtr) // New stack is [..., condPtr, junkPtr]
			gen.pushLoopStart(junkPtr)
			gen.sb.WriteRune('[')

			// This code applies when the condition is true...
			gen.sb.WriteString("[-]") // Clear condPtr to break loop
			gen.point(junkPtr)
			gen.sb.WriteString("[-]") // Clear junkPtr to prevent else statement
		case "else":
			junkPtr := gen.popLoopStart()
			condPtr := gen.popLoopStart()
			gen.point(condPtr)    // Go back to the condPtr to exit the loop
			gen.sb.WriteRune(']') // Exit loop

			gen.point(junkPtr)
			gen.sb.WriteRune('[') // If the condition was false, the junkPtr will activate the loop

			gen.pushLoopStart(condPtr) // Still have to push our addresses for the endif
			gen.pushLoopStart(junkPtr)
		case "endif":
			_ = gen.popLoopStart()        // Pop junkPtr
			condPtr := gen.popLoopStart() // Pop condPtr
			gen.point(condPtr)            // Go back to condPtr (notice how 'else' also does this at the end)
			gen.sb.WriteRune(']')
		default:
			panic("not a valid instruction name: " + instr.name)
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
		for i, in := range ast.instr {
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
