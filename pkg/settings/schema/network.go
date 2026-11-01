package schema

import "encoding/json"

type Network struct {
	Verify  bool   `json:"verify"`
	ViaHome bool   `json:"via_home,omitempty"`
	Address string `json:"address,omitempty"`
	HomeKey string `json:"home_key,omitempty"`
}

func DefaultNetwork() Network { return Network{Verify: true} }

func MigrateNetwork(data []byte, network *Network) error {
	var old struct {
		Network struct {
			Verify *bool `json:"verify"`
		} `json:"network"`
		Diag struct {
			InsecureTLS *bool `json:"insecure_tls"`
		} `json:"diag"`
	}
	if err := json.Unmarshal(data, &old); err != nil {
		return err
	}
	if old.Network.Verify == nil && old.Diag.InsecureTLS != nil {
		network.Verify = !*old.Diag.InsecureTLS
	}
	return nil
}
