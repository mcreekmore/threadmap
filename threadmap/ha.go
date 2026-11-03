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

// matterTemplate maps "<fabric ID>-<node ID>", the Matter mDNS instance name, to the HA device name.
// HA stores it in the identifier deviceid_<fabric ID>-<node ID>-MatterNodeDevice.
const matterTemplate = `{% set ns = namespace(out={}) %}
{%- for d in integration_entities('matter') | map('device_id') | reject('none') | unique %}
{%- for i in device_attr(d, 'identifiers') if i[0] == 'matter' and i[1].startswith('deviceid_') and i[1].endswith('-MatterNodeDevice') %}
{%- set ns.out = dict(ns.out, **{i[1][9:-17]: device_attr(d, 'name_by_user') or device_attr(d, 'name')}) %}
{%- endfor %}{% endfor %}{{ ns.out | tojson }}`

// matterNames returns nil when HA can't be asked, so callers keep the raw IDs instead of dropping them.
func matterNames(ctx context.Context, base, token string) map[string]string {
	if token == "" {
		return nil
	}
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
	var names map[string]string
	if err == nil {
		err = json.NewDecoder(resp.Body).Decode(&names)
	}
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		log.Printf("Matter names from Home Assistant: %v", err)
		return nil
	}
	return names
}

// nameMatter swaps Matter IDs for HA names. IDs HA doesn't know belong to other controllers' fabrics, so they're dropped.
func nameMatter(services []service, names map[string]string) []service {
	if names == nil {
		return services
	}
	var out []service
	for _, s := range services {
		if !s.matter {
			out = append(out, s)
		} else if name, ok := names[strings.ToUpper(s.name)]; ok {
			s.name = name
			out = append(out, s)
		}
	}
	return out
}
