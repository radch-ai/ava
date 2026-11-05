// Package apitypes is the API wire contract shared by backend and
// webui: tygo generates ui/web/src/lib/types/gen/apitypes.gen.ts from
// these types (`just typegen`); that file is gitignored and
// regenerated, never edited. One file per wire concern.
package apitypes

// AgentIdentity is the id plus display fields; list entries and
// detail bodies both carry it.
type AgentIdentity struct {
	// AgentID is the agent id used in URLs and API paths;
	// `_`-prefixed ids are system-reserved.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Icon is an icon name; the UI narrows it to its AgentIconName
	// union — the wire carries any string.
	Icon string `json:"icon"`
}
