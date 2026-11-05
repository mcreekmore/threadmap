package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

const coreAPI = "http://172.30.32.2/core/api" // Supervisor's proxy to the HA Core API.

// matterTemplate maps "<fabric ID>-<node ID>", the Matter mDNS instance name, to the HA device.
// HA stores it in the identifier deviceid_<fabric ID>-<node ID>-MatterNodeDevice.
const matterTemplate = `{% set ns = namespace(out={}) %}
{%- for d in integration_entities('matter') | map('device_id') | reject('none') | unique %}
{%- for i in device_attr(d, 'identifiers') if i[0] == 'matter' and i[1].startswith('deviceid_') and i[1].endswith('-MatterNodeDevice') %}
{%- set ns.out = dict(ns.out, **{i[1][9:-17]: {
  'id': d,
  'name': device_attr(d, 'name_by_user') or device_attr(d, 'name'),
  'manufacturer': device_attr(d, 'manufacturer'),
  'model': device_attr(d, 'model'),
  'firmware': device_attr(d, 'sw_version'),
  'area': area_name(d),
}}) %}
{%- endfor %}{% endfor %}{{ ns.out | tojson }}`

// haDevice is a device in HA's device registry. Fields HA doesn't know are empty.
type haDevice struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	Firmware     string `json:"firmware,omitempty"`
	Area         string `json:"area,omitempty"`
}

// matterDevices returns nil when HA can't be asked, so callers keep the raw IDs instead of dropping them.
func matterDevices(ctx context.Context, base, token string) map[string]haDevice {
	if token == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, requestLimit) // A hung HA would otherwise hold the whole scan.
	defer cancel()
	body, _ := json.Marshal(map[string]string{"template": matterTemplate})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/template", strings.NewReader(string(body)))
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err == nil && resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("%s", resp.Status)
	}
	var devices map[string]haDevice
	if err == nil {
		err = json.NewDecoder(resp.Body).Decode(&devices)
	}
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		log.Printf("Matter devices from Home Assistant: %v", err)
		return nil
	}
	return devices
}

// nameMatter swaps Matter IDs for HA devices. IDs HA doesn't know belong to other controllers' fabrics, so they're dropped.
func nameMatter(services []service, devices map[string]haDevice) []service {
	if devices == nil {
		return services
	}
	var out []service
	for _, s := range services {
		if !s.matter {
			out = append(out, s)
		} else if d, ok := devices[strings.ToUpper(s.name)]; ok {
			s.name, s.ha = d.Name, &d
			out = append(out, s)
		}
	}
	return out
}
