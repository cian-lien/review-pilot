package contracts

import (
	"encoding/json"
	"testing"
)

func TestReviewNullReferencesRemainNull(t *testing.T) {
	var payload ReviewStagePayload
	input := []byte(`{"context":null,"candidates":null,"verification":null}`)
	if err := DecodeReview("ReviewStagePayload", input, &payload, 256); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var facts map[string]any
	if json.Unmarshal(body, &facts) != nil {
		t.Fatal("invalid serialized stage")
	}
	for _, name := range []string{"context", "candidates", "verification"} {
		if facts[name] != nil {
			t.Fatalf("null reference %s became an empty typed object", name)
		}
	}
}
