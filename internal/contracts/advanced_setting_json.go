package contracts

import "encoding/json"

func (a *AdvancedSetting) UnmarshalJSON(data []byte) error {
	type alias AdvancedSetting
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := raw["tengu_glacier_2xr"]; !ok {
		if value, aliasOK := raw["tenguGlacier2xr"]; aliasOK {
			raw["tengu_glacier_2xr"] = value
		}
	}
	normalized, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	var out alias
	if err := json.Unmarshal(normalized, &out); err != nil {
		return err
	}
	*a = AdvancedSetting(out)
	return nil
}
