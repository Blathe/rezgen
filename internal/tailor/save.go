package tailor

import "encoding/json"

// Name suggests an application name from an analysis: "Company - Role".
func (a *Analysis) Name() string {
	switch {
	case a.Company != "" && a.Role != "":
		return a.Company + " - " + a.Role
	case a.Company != "":
		return a.Company
	case a.Role != "":
		return a.Role
	}
	return "Application"
}

func toJSON(v any) string {
	data, _ := json.MarshalIndent(v, "", "  ")
	return string(data) + "\n"
}
