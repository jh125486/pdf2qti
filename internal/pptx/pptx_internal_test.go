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
			name:  "open/close bodyPr with an unrelated child and no autofit child gets normAutofit inserted",
			block: `<p:sp><p:txBody><a:bodyPr anchor="ctr"><a:prstTxWarp prst="textNoShape"/></a:bodyPr><a:lstStyle/></p:txBody></p:sp>`,
			want:  `<p:sp><p:txBody><a:bodyPr anchor="ctr"><a:normAutofit/><a:prstTxWarp prst="textNoShape"/></a:bodyPr><a:lstStyle/></p:txBody></p:sp>`,
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
