package campaign

import "testing"

func TestExportTextPreventsSpreadsheetFormula(t *testing.T) {
	for _, input := range []string{"=CMD()", "+1+1", "-2+3", "@SUM(A1:A2)", "  =CMD()"} {
		got := exportText(input)
		if got == input || got[0] != '\'' {
			t.Fatalf("input %q was not escaped: %q", input, got)
		}
	}
}
