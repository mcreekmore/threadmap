package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestMatterDevices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if r.Method != http.MethodPost || r.URL.Path != "/template" || r.Header.Get("Authorization") != "Bearer tok" || !strings.Contains(body["template"], "MatterNodeDevice") {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"74F6D4610D01F2BA-0000000000000001":{"id":"d1","name":"Smart Night Light Thread","manufacturer":"Meross","model":null,"firmware":"1.0","area":"Bedroom"}}`))
	}))
	defer srv.Close()

	got := matterDevices(context.Background(), srv.URL, "tok")
	want := map[string]haDevice{"74F6D4610D01F2BA-0000000000000001": {ID: "d1", Name: "Smart Night Light Thread", Manufacturer: "Meross", Firmware: "1.0", Area: "Bedroom"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("devices = %v", got)
	}
	if matterDevices(context.Background(), srv.URL, "wrong") != nil || matterDevices(context.Background(), srv.URL, "") != nil {
		t.Error("expected nil when HA can't be asked")
	}
}

func TestNameMatter(t *testing.T) {
	services := []service{
		{name: "74F6D4610D01F2BA-0000000000000001", matter: true},
		{name: "459FCEB0616EBF74-000000006C54EB63", matter: true}, // Another controller's fabric.
		{name: "Airversa AH1 2122"},
	}
	devices := map[string]haDevice{"74F6D4610D01F2BA-0000000000000001": {ID: "d1", Name: "Smart Night Light Thread"}}

	named := nameMatter(services, devices)
	var got []string
	for _, s := range named {
		got = append(got, s.name)
	}
	if want := []string{"Smart Night Light Thread", "Airversa AH1 2122"}; !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v", got)
	}
	if named[0].ha == nil || named[0].ha.ID != "d1" || named[1].ha != nil {
		t.Errorf("only the Matter service should carry its HA device, got %v and %v", named[0].ha, named[1].ha)
	}
	if len(nameMatter(services, nil)) != 3 {
		t.Error("without HA names, every service should be kept")
	}
}
