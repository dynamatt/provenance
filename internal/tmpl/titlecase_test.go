package tmpl

import "testing"

func TestTitleCase(t *testing.T) {
	for in, want := range map[string]string{
		"VerificationSummaryReport": "Verification Summary Report",
		"UserNeed":                  "User Need",
		"Requirement":               "Requirement",
		"verified_by":               "Verified By",
		"equipment_used":            "Equipment Used",
		"reported_in":               "Reported In",
		"bill_of_materials":         "Bill of Materials",
		"SBOMEntry":                 "SBOM Entry",
		"ISO13485Clause":            "ISO13485 Clause",
		"ECO":                       "ECO",
		"step-results":              "Step Results",
		"a_list_of":                 "A List Of",
		"":                          "",
	} {
		if got := TitleCase(in); got != want {
			t.Errorf("TitleCase(%q) = %q, want %q", in, got, want)
		}
	}
}
