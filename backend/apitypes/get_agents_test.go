package apitypes

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/radch-ai/ava/backend/internal/db"
)

// The sqlc rows the list endpoint returns serialize byte-identically
// to the wire identity — the response is built from rows as-is.
func TestGetAgentsRowMatchesIdentity(t *testing.T) {
	rows := []db.GetAgentsRow{
		{ID: "scribe", Name: "Scribe", Icon: "pencil"},
		{ID: "research", Name: "Research", Icon: "globe"},
	}
	identities := make([]AgentIdentity, len(rows))
	for i, row := range rows {
		// Compiles only while both structs stay field-for-field
		// identical; the marshal below pins the json tags.
		identities[i] = AgentIdentity(row)
	}
	rowsJSON, err := json.Marshal(GetAgentsResponse{Agents: rows})
	if err != nil {
		t.Fatalf("marshal rows response: %v", err)
	}
	identitiesJSON, err := json.Marshal(struct {
		Agents []AgentIdentity `json:"agents"`
	}{Agents: identities})
	if err != nil {
		t.Fatalf("marshal identities response: %v", err)
	}
	if !bytes.Equal(rowsJSON, identitiesJSON) {
		t.Errorf("rows body = %s, identities body = %s", rowsJSON, identitiesJSON)
	}
	want := `{"agents":[{"id":"scribe","name":"Scribe","icon":"pencil"},{"id":"research","name":"Research","icon":"globe"}]}`
	if string(rowsJSON) != want {
		t.Errorf("body = %s, want %s", rowsJSON, want)
	}
}
