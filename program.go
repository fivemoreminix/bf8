package main

import (
	"bytes"
	"errors"
	"fmt"
	"time"
)

type Opcode byte

// 0 - 19 Common
const (
	OpNop       Opcode = iota
	OpRelJmpFwd        // 1 byte IN; pc += byte
	OpRelJmpBwd        // 1 byte IN; pc -= byte
)

// 20 - 39 Data and Registers
const (
	OpR8AStore  Opcode = 20 + iota // 1 byte IN; r8a = byte
	OpR8BStore                     // 1 byte IN; r8b = byte
	OpR16AStore                    // 2 byte IN; r16a = 2 byte
	OpR16BStore                    // 2 byte IN; r16b = 2 byte
	OpR32AStore                    // 4 byte IN; r32a = 4 byte
	OpR32BStore                    // 4 byte IN; r32b = 4 byte
	OpR8ALoad                      // 1 byte OUT; 1 byte = r8a
	OpR8BLoad                      // 1 byte OUT; 1 byte = r8b
	OpR16ALoad                     // 2 byte OUT; 2 byte = r16a
	OpR16BLoad                     // 2 byte OUT; 2 byte = r16b
	OpR32ALoad                     // 4 byte OUT; 4 byte = r32a
	OpR32BLoad                     // 4 byte OUT; 4 byte = r32b
)

// 40 - 59 Graphics Drawing
const (
	OpClearCanvas Opcode = 40 + iota
	OpSetColor           // 4 byte IN; r, g, b, a (non-alpha-premultiplied color)
	OpSetPixel           // 2 byte IN; x, y
	OpDrawLine           // 4 byte IN; x1, y1, x2, y2
)

type Op struct {
	code Opcode
	args [8]byte
}

// Byte returns the byte at args[-i] in Python notation.
//
// Byte(0) returns the last byte in args.
func (op Op) Byte(i int) byte {
	return op.args[len(op.args)-i-1]
}

func (op Op) Word(i int) uint16 {
	return uint16(op.Byte(1))<<8 | uint16(op.Byte(0))
}

func (op Op) QWord(i int) uint32 {
	return uint32(op.Byte(3))<<24 | uint32(op.Byte(2))<<16 |
		uint32(op.Byte(1))<<8 | uint32(op.Byte(0))
}

var (
	ErrProgramNoMemory      = errors.New("Program memory not initialized")
	ErrCodeBracketImbalance = errors.New("brainfuck loop start/ends are out of balance")
)

func ValidateBrainfuck(code []byte) error {
	depth := 0
	for i := range code {
		switch code[i] {
		case '[':
			depth++
		case ']':
			depth--
		}
	}
	if depth != 0 {
		return ErrCodeBracketImbalance
	}
	return nil
}

type Program struct {
	// Virtual machine registers

	memory    []byte
	dataStart int           // Index of the data section and where memPtr starts.
	clockRate time.Duration // Limit the time to compute a Brainfuck instruction.
	pc        int
	r8a       byte
	r8b       byte
	r16a      uint16
	r16b      uint16
	r32a      uint32
	r32b      uint32

	// Brainfuck program specific

	memPtr int // The pointer to memory that the Brainfuck program manipulates using > and <
}

func NewProgram(code []byte) (*Program, error) {
	if err := ValidateBrainfuck(code); err != nil {
		return nil, err
	}

	code = bytes.Map(func(r rune) rune {
		if r == '.' || r == ',' || r == '+' || r == '-' || r == '>' || r == '<' ||
			r == '[' || r == ']' {
			return r
		}
		return -1 // Drop any bytes that are not Brainfuck instructions
	}, code)

	dataStart := len(code) + 10
	p := &Program{
		memory:    make([]byte, dataStart+30_000),
		dataStart: dataStart,
		pc:        0,

		memPtr: dataStart,
	}
	// Copy code to the beginning of the memory
	copy(p.memory, code)

	return p, nil
}

func (p *Program) CodeSection() []byte {
	return p.memory[:p.dataStart]
}

func (p *Program) DataSection() []byte {
	return p.memory[p.dataStart:]
}

func (p *Program) Right() {
	p.memPtr++
	// Wrap around to the beginning
	if p.memPtr >= len(p.memory) {
		p.memPtr = 0
	}
}

func (p *Program) Left() {
	p.memPtr--
	// Wrap around to the end
	if p.memPtr < 0 {
		p.memPtr = len(p.memory) - 1
	}
}

// Byte returns the byte at p.memory[idx].
func (p *Program) Byte(idx int) byte {
	return p.memory[idx]
}

// SetByte assigns the byte at p.memory[idx] to value.
func (p *Program) SetByte(idx int, value byte) {
	p.memory[idx] = value
}

func (p *Program) Word(idx int) uint16 {
	return uint16(p.Byte(idx-1))<<8 | uint16(p.Byte(idx))
}

func (p *Program) SetWord(idx int, value uint16) {
	p.SetByte(idx-1, byte(value>>8))
	p.SetByte(idx, byte(value))
}

func (p *Program) QWord(idx int) uint32 {
	return uint32(p.Byte(idx-3))<<24 | uint32(p.Byte(idx-2))<<16 |
		uint32(p.Byte(idx-1))<<8 | uint32(p.Byte(idx))
}

func (p *Program) SetQWord(idx int, value uint32) {
	p.SetByte(idx-3, byte(value>>24))
	p.SetByte(idx-2, byte(value>>16))
	p.SetByte(idx-1, byte(value>>8))
	p.SetByte(idx, byte(value))
}

func (p *Program) JumpToCloseLoop() {
	depth := 0
	instr := p.memory[p.pc]
	for instr != 0 {
		switch instr {
		case '[':
			depth++
		case ']':
			depth--

			if depth <= 0 {
				return
			}
		}
		p.pc++
		instr = p.memory[p.pc]
	}
}

func (p *Program) JumpToOpenLoop() {
	depth := 0
	instr := p.memory[p.pc]
	for instr != 0 {
		switch instr {
		case ']':
			depth++
		case '[':
			depth--

			if depth <= 0 {
				return
			}
		}
		p.pc--
		// TODO: What happens when self-modifying code causes a loop imbalance...
		if p.pc < 0 {
			p.pc = 0
			return
		}
		instr = p.memory[p.pc]
	}
}

func (p *Program) Op(op Op, opChan chan Op) {
	switch op.code {
	case OpNop:
	case OpRelJmpFwd:
		p.pc += int(op.Byte(0))
		// I'm pretty sure this would just cause program termination anyway...
		if p.pc >= len(p.memory) {
			p.pc = len(p.memory) - 1
		}
	case OpRelJmpBwd:
		p.pc -= int(op.Byte(0))
		if p.pc < 0 {
			p.pc = 0
		}
	case OpR8AStore:
		p.r8a = op.Byte(0)
	case OpR8BStore:
		p.r8b = op.Byte(0)
	case OpR16AStore:
		p.r16a = op.Word(0)
	case OpR16BStore:
		p.r16b = op.Word(0)
	case OpR32AStore:
		p.r32a = op.QWord(0)
	case OpR32BStore:
		p.r32b = op.QWord(0)
	case OpR8ALoad:
		p.SetByte(p.memPtr-1, p.r8a)
	case OpR8BLoad:
		p.SetByte(p.memPtr-1, p.r8b)
	case OpR16ALoad:
		p.SetWord(p.memPtr-1, p.r16a)
	case OpR16BLoad:
		p.SetWord(p.memPtr-1, p.r16b)
	case OpR32ALoad:
		p.SetQWord(p.memPtr-1, p.r32a)
	case OpR32BLoad:
		p.SetQWord(p.memPtr-1, p.r32b)
	default:
		opChan <- op
	}
}

// Run blocks the thread that the function has been called on until program termination.
func (p *Program) Run(opChan chan Op) error {
	if len(p.memory) == 0 {
		return ErrProgramNoMemory
	}

	var instr byte = p.memory[p.pc]
	for instr != 0 {
		start := time.Now()

		switch instr {
		case '>':
			var i int
			for i = p.pc; p.memory[i] == '>'; i++ {
				p.Right()
			}
			p.pc = i - 1 // When p.memory[i] != '>' then we've overstepped
		case '<':
			var i int
			for i = p.pc; p.memory[i] == '<'; i++ {
				p.Left()
			}
			p.pc = i - 1
		case '+':
			var amt int

			var i int
			for i = p.pc; p.memory[i] == '+'; i++ {
				amt++
			}
			p.pc = i - 1

			newVal := AddRolling(int(p.Byte(p.memPtr)), amt, 255)
			p.SetByte(p.memPtr, byte(newVal))
		case '-':
			var amt int

			var i int
			for i = p.pc; p.memory[i] == '-'; i++ {
				amt++
			}
			p.pc = i - 1

			newVal := SubRolling(int(p.Byte(p.memPtr)), amt, 255)
			p.SetByte(p.memPtr, byte(newVal))
		case '[':
			if bytes.Equal(p.memory[p.pc:p.pc+3], []byte("[-]")) {
				// Clear the cell when a [-] is encountered.
				p.SetByte(p.memPtr, 0)
				p.pc += 2
			} else if p.Byte(p.memPtr) == 0 {
				p.JumpToCloseLoop()
			}
		case ']':
			if p.Byte(p.memPtr) != 0 {
				p.JumpToOpenLoop()
			}
		case '.':
			op := Op{
				code: Opcode(p.Byte(p.memPtr)),
				args: [8]byte{},
			}
			argsStart := p.memPtr - 8
			if p.memPtr-8 < 0 {
				argsStart = 0
			}
			// FIXME: args are copied leaving blank space at end if argsStart = 0
			copy(op.args[:], p.memory[argsStart:p.memPtr])
			p.Op(op, opChan)
		case ',':
		}

		// Get the next Brainfuck instruction
		p.pc++
		instr = p.memory[p.pc]

		if p.clockRate > 1 { // If the clockRate > 1 nanosecond
			elapsed := time.Since(start)
			if elapsed < p.clockRate {
				duration := p.clockRate - elapsed
				fmt.Printf("%v elapsed, sleeping for %v\n", elapsed, duration)
				time.Sleep(duration)
			}
		}
	}

	return nil
}

func AddRolling(n, amt, max int) int {
	return (n + amt) % max
}

func SubRolling(n, amt, max int) int {
	return (n - amt) % max
}
