package schema

import "encoding/json"

type Access struct {
	ADB bool `json:"adb"`
}

func MigrateAccess(data []byte, access *Access) error {
	var old struct {
		Access *json.RawMessage `json:"access"`
		Diag   struct {
			ADB bool `json:"remote_adb"`
		} `json:"diag"`
	}
	if err := json.Unmarshal(data, &old); err != nil {
		return err
	}
	if old.Access == nil && old.Diag.ADB {
		access.ADB = true
	}
	return nil
}
