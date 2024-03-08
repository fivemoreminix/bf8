package main

import (
	"image/color"
	"os"
	"time"

	"github.com/fivemoreminix/bf8/vm"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	screenWidth, screenHeight = 255, 191
)

type System struct {
	program *vm.Program
	opChan  chan vm.Op

	canvas *ebiten.Image
	color  color.NRGBA

	didInit bool
}

func (s *System) init() {
	go s.program.Run(s.opChan)
}

func (s *System) Update() error {
	// Ensures that Ebitengine is ready before we run the program
	if !s.didInit {
		s.init()
		s.didInit = true
	}

	limit := 60
loop:
	for range limit {
		select {
		case op := <-s.opChan:
			switch op.Code {
			case vm.OpClearCanvas:
				s.canvas.Clear()
			case vm.OpSetColor:
				s.color.R = op.Byte(3)
				s.color.G = op.Byte(2)
				s.color.B = op.Byte(1)
				s.color.A = op.Byte(0)
			case vm.OpSetPixel:
				x := op.Byte(1)
				y := op.Byte(0)
				s.canvas.Set(int(x), int(y), s.color)
			case vm.OpDrawLine:
				x1 := float32(op.Byte(3))
				y1 := float32(op.Byte(2))
				x2 := float32(op.Byte(1))
				y2 := float32(op.Byte(0))
				vector.StrokeLine(s.canvas, x1, y1, x2, y2, 1, s.color, false)
			}
		default:
			break loop
		}
	}

	return nil
}

func (s *System) Draw(screen *ebiten.Image) {
	// Graphics...
	// ebitenutil.DebugPrint(screen, "test")
	screen.DrawImage(s.canvas, &ebiten.DrawImageOptions{})
}

func (s *System) Layout(_outsideWidth, _outsideHeight int) (int, int) {
	return screenWidth, screenHeight
}

func main() {
	bytes, err := os.ReadFile("boot.bf")
	if err != nil {
		panic(err)
	}

	program, err := vm.NewProgram(bytes)
	if err != nil {
		panic(err)
	}

	// Brainfuck is only truly as fast as we can handle its Operations. Increasing the channel
	// size helps to keep it from blocking, but also handling more operations per Update.

	program.ClockRate = time.Millisecond // One brainfuck instruction every millisecond

	system := &System{
		program: program,
		opChan:  make(chan vm.Op, 256), // Channels must be buffered to do non-blocking reads

		canvas: ebiten.NewImage(screenWidth, screenHeight),
		color:  color.NRGBA{},
	}

	ebiten.SetWindowSize(screenWidth*3, screenHeight*3)
	ebiten.SetWindowTitle("bf8")
	if err := ebiten.RunGame(system); err != nil {
		panic(err)
	}
}
