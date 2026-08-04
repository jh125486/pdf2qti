// Whitebox package: ensureNormAutofit is unexported.
package pptx

import "testing"

func TestEnsureNormAutofit_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		block string
		want  string
	}{
		{
			name:  "self-closing empty bodyPr gets normAutofit inserted",
			block: `<p:sp><p:txBody><a:bodyPr/><a:lstStyle/></p:txBody></p:sp>`,
			want:  `<p:sp><p:txBody><a:bodyPr><a:normAutofit/></a:bodyPr><a:lstStyle/></p:txBody></p:sp>`,
		},
		{
			name:  "self-closing bodyPr with attributes keeps them",
			block: `<p:sp><p:txBody><a:bodyPr anchor="ctr" wrap="square"/><a:lstStyle/></p:txBody></p:sp>`,
			want:  `<p:sp><p:txBody><a:bodyPr anchor="ctr" wrap="square"><a:normAutofit/></a:bodyPr><a:lstStyle/></p:txBody></p:sp>`,
		},
		{
			// CT_TextBodyProperties's schema requires prstTxWarp, when present, to precede the
			// autofit choice — normAutofit must be inserted after it, not as bodyPr's first child.
			name:  "open/close bodyPr with a prstTxWarp child keeps prstTxWarp before the inserted normAutofit",
			block: `<p:sp><p:txBody><a:bodyPr anchor="ctr"><a:prstTxWarp prst="textNoShape"/></a:bodyPr><a:lstStyle/></p:txBody></p:sp>`,
			want:  `<p:sp><p:txBody><a:bodyPr anchor="ctr"><a:prstTxWarp prst="textNoShape"/><a:normAutofit/></a:bodyPr><a:lstStyle/></p:txBody></p:sp>`,
		},
		{
			name:  "open/close bodyPr with a prstTxWarp that has children keeps it intact before normAutofit",
			block: `<p:sp><p:txBody><a:bodyPr><a:prstTxWarp prst="textArchUp"><a:avLst><a:gd name="adj" fmla="val 5400000"/></a:avLst></a:prstTxWarp></a:bodyPr><a:lstStyle/></p:txBody></p:sp>`,
			want:  `<p:sp><p:txBody><a:bodyPr><a:prstTxWarp prst="textArchUp"><a:avLst><a:gd name="adj" fmla="val 5400000"/></a:avLst></a:prstTxWarp><a:normAutofit/></a:bodyPr><a:lstStyle/></p:txBody></p:sp>`,
		},
		{
			name:  "bodyPr already declaring normAutofit is left untouched",
			block: `<p:sp><p:txBody><a:bodyPr><a:normAutofit/></a:bodyPr><a:lstStyle/></p:txBody></p:sp>`,
			want:  `<p:sp><p:txBody><a:bodyPr><a:normAutofit/></a:bodyPr><a:lstStyle/></p:txBody></p:sp>`,
		},
		{
			name:  "bodyPr already declaring noAutofit is left untouched",
			block: `<p:sp><p:txBody><a:bodyPr><a:noAutofit/></a:bodyPr><a:lstStyle/></p:txBody></p:sp>`,
			want:  `<p:sp><p:txBody><a:bodyPr><a:noAutofit/></a:bodyPr><a:lstStyle/></p:txBody></p:sp>`,
		},
		{
			name:  "bodyPr already declaring spAutoFit is left untouched",
			block: `<p:sp><p:txBody><a:bodyPr><a:spAutoFit/></a:bodyPr><a:lstStyle/></p:txBody></p:sp>`,
			want:  `<p:sp><p:txBody><a:bodyPr><a:spAutoFit/></a:bodyPr><a:lstStyle/></p:txBody></p:sp>`,
		},
		{
			name:  "no bodyPr at all is left untouched",
			block: `<p:sp><p:txBody><a:lstStyle/></p:txBody></p:sp>`,
			want:  `<p:sp><p:txBody><a:lstStyle/></p:txBody></p:sp>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := string(ensureNormAutofit([]byte(tt.block))); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
