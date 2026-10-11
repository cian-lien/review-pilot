package publisher

import (
	"reviewpilot.local/api/internal/contracts"
	"testing"
)

func TestReviewPublicationSelectsAddedLine(t *testing.T) {
	patch := "@@ -1,2 +1,3 @@\n keep();\n+charge();\n unchanged();"
	pages := []contracts.ReviewContextPage{{Files: []contracts.ReviewChangedFile{{Path: "pay.ts", Head: contracts.ReviewFileContent{Sha: "head"}, Patch: &patch}}}}
	ref := contracts.EvidenceRef{Path: "pay.ts", Sha: "head", StartLine: 1, EndLine: 3}
	if line := publicationLine(pages, ref); line == nil || *line != 2 {
		t.Fatal("unchanged end of evidence was used as an inline anchor")
	}
	ref.StartLine = 3
	if publicationLine(pages, ref) != nil {
		t.Fatal("unchanged location became inline")
	}
}
