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

// deviceTemplate lists the HA devices mDNS services can be matched to, keyed by what each service advertises:
//   - Matter: the instance name "<fabric ID>-<node ID>", from the identifier deviceid_<fabric ID>-<node ID>-MatterNodeDevice.
//   - HomeKit: the pairing ID from the TXT record, from the identifier <pairing ID>:aid:1 (aid 1 is the device itself).
const deviceTemplate = `{% set ns = namespace(matter={}, homekit={}) %}
{%- for d in (integration_entities('matter') + integration_entities('homekit_controller')) | map('device_id') | reject('none') | unique %}
{%- set device = {
  'id': d,
  'name': device_attr(d, 'name_by_user') or device_attr(d, 'name'),
  'manufacturer': device_attr(d, 'manufacturer'),
  'model': device_attr(d, 'model'),
  'firmware': device_attr(d, 'sw_version'),
  'area': area_name(d),
} %}
{%- for i in device_attr(d, 'identifiers') %}
{%- if i[0] == 'matter' and i[1].startswith('deviceid_') and i[1].endswith('-MatterNodeDevice') %}
{%- set ns.matter = dict(ns.matter, **{i[1][9:-17]: device}) %}
{%- elif i[0] == 'homekit_controller:accessory-id' and i[1].endswith(':aid:1') %}
{%- set ns.homekit = dict(ns.homekit, **{i[1][:-6] | upper: device}) %}
{%- endif %}
{%- endfor %}{% endfor %}{{ {'matter': ns.matter, 'homekit': ns.homekit} | tojson }}`

// haDevice is a device in HA's device registry. Fields HA doesn't know are empty.
type haDevice struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	Firmware     string `json:"firmware,omitempty"`
	Area         string `json:"area,omitempty"`
}

type haDevices struct {
	Matter  map[string]haDevice `json:"matter"`  // by "<fabric ID>-<node ID>"
	HomeKit map[string]haDevice `json:"homekit"` // by pairing ID
}

// fetchHADevices returns nil when HA can't be asked, so callers keep the raw Matter IDs instead of dropping them.
func fetchHADevices(ctx context.Context, base, token string) *haDevices {
	if token == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, requestLimit) // A hung HA would otherwise hold the whole scan.
	defer cancel()
	body, _ := json.Marshal(map[string]string{"template": deviceTemplate})
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
	var devices *haDevices
	if err == nil {
		err = json.NewDecoder(resp.Body).Decode(&devices)
	}
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		log.Printf("Devices from Home Assistant: %v", err)
		return nil
	}
	return devices
}

// matchHA gives each service its HA device and name. Matter IDs HA doesn't know belong to other
// controllers' fabrics, so they're dropped; HomeKit devices HA doesn't know keep their own name.
func matchHA(services []service, devices *haDevices) []service {
	if devices == nil {
		return services
	}
	var out []service
	for _, s := range services {
		var d haDevice
		var ok bool
		if s.matter {
			if d, ok = devices.Matter[strings.ToUpper(s.name)]; !ok {
				continue
			}
		} else {
			d, ok = devices.HomeKit[s.hapID]
		}
		if ok {
			s.name, s.ha = d.Name, &d
		}
		out = append(out, s)
	}
	return out
}
