package main

import "testing"

func TestProgramRun(t *testing.T) {
	table := []struct {
		input   []byte
		wantMem []byte // wantMem must match the first len(wantMem) bytes of Program.Data() section.
	}{
		{input: []byte("add 5 +++++"), wantMem: []byte{5}},
		{input: []byte(">>>>+<-"), wantMem: []byte{0, 0, 0, 255, 1}},
		{input: []byte("+++++ +++++[>+++++ +++++<-] 100"), wantMem: []byte{0, 100}},
		{input: []byte("+++[[>]+++++[<]>-]"), wantMem: []byte{0, 5, 5, 5}},

		// Opcodes
		{input: []byte("++>+. ++ +"), wantMem: []byte{2, 2}}, // OpJmpRelFwd
		{ // OpReg16AStore, OpReg16ALoad
			input:   []byte("+>++++>++++++++++ ++++++++++ ++.[>>+<<-]>>++++++."),
			wantMem: []byte{1, 4, 1, 4, 28}, // {1, 4} is 0b1_0000_0100 or 260
		},
	}

	for _, test := range table {
		testName := string(test.input)
		t.Run(testName, func(t *testing.T) {
			p, err := NewProgram(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if err = p.Run(nil); err != nil {
				t.Error(err)
			}

			// Compare expected data to actual data
			dataSection := p.DataSection()
			for i := range test.wantMem {
				if dataSection[i] != test.wantMem[i] {
					t.Errorf("dataSection[%d] (%d) != wantMem[%d] (%d)",
						i, dataSection[i], i, test.wantMem[i])
				}
			}

			if t.Failed() {
				t.Logf("dataSection[:len(wantMem)] =\n%v", dataSection[:len(test.wantMem)])
			}
		})
	}
}
