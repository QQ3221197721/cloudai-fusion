package redteam

import "encoding/json"

// navigator.go exports an engagement report's exercised techniques as a MITRE
// ATT&CK Navigator layer (the standard JSON consumed by the ATT&CK Navigator UI),
// so findings map straight onto the ATT&CK matrix for reporting.

type navTechnique struct {
	TechniqueID string `json:"techniqueID"`
	Score       int    `json:"score"`
	Color       string `json:"color"`
	Comment     string `json:"comment"`
}

type navLayer struct {
	Name     string `json:"name"`
	Versions struct {
		Attack    string `json:"attack"`
		Navigator string `json:"navigator"`
		Layer     string `json:"layer"`
	} `json:"versions"`
	Domain      string         `json:"domain"`
	Description string         `json:"description"`
	Techniques  []navTechnique `json:"techniques"`
}

// NavigatorLayer renders the report's techniques as an ATT&CK Navigator layer.
func NavigatorLayer(rep *Report) ([]byte, error) {
	l := navLayer{
		Name:        "CloudAI Fusion Red Team - " + rep.EngagementID,
		Domain:      "enterprise-attack",
		Description: "Techniques exercised during authorized engagement " + rep.EngagementID,
	}
	l.Versions.Attack = "14"
	l.Versions.Navigator = "4.9.1"
	l.Versions.Layer = "4.5"
	for _, tech := range rep.Techniques {
		l.Techniques = append(l.Techniques, navTechnique{
			TechniqueID: tech,
			Score:       1,
			Color:       "#e60d0d",
			Comment:     "exercised (verifiable via signed evidence)",
		})
	}
	return json.MarshalIndent(l, "", "  ")
}
