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

func TestFetchHADevices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if r.Method != http.MethodPost || r.URL.Path != "/template" || r.Header.Get("Authorization") != "Bearer tok" || !strings.Contains(body["template"], "MatterNodeDevice") {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"matter":{"74F6D4610D01F2BA-0000000000000001":{"id":"d1","name":"Smart Night Light Thread","manufacturer":"ThirdReality","model":null,"firmware":"1.0","area":"Bedroom"}},
			"homekit":{"43:3E:01:0F:92:53":{"id":"d2","name":"Airversa AH1 2122","area":null}}}`))
	}))
	defer srv.Close()

	got := fetchHADevices(context.Background(), srv.URL, "tok")
	want := &haDevices{
		Matter:  map[string]haDevice{"74F6D4610D01F2BA-0000000000000001": {ID: "d1", Name: "Smart Night Light Thread", Manufacturer: "ThirdReality", Firmware: "1.0", Area: "Bedroom"}},
		HomeKit: map[string]haDevice{"43:3E:01:0F:92:53": {ID: "d2", Name: "Airversa AH1 2122"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("devices = %+v", got)
	}
	if fetchHADevices(context.Background(), srv.URL, "wrong") != nil || fetchHADevices(context.Background(), srv.URL, "") != nil {
		t.Error("expected nil when HA can't be asked")
	}
}

func TestMatchHA(t *testing.T) {
	services := []service{
		{name: "74F6D4610D01F2BA-0000000000000001", matter: true},
		{name: "459FCEB0616EBF74-000000006C54EB63", matter: true}, // Another controller's fabric.
		{name: "Airversa AH1 2122", hapID: "43:3E:01:0F:92:53"},
		{name: "Unpaired Lamp", hapID: "11:22:33:44:55:66"}, // HomeKit, but not added to HA.
	}
	devices := &haDevices{
		Matter:  map[string]haDevice{"74F6D4610D01F2BA-0000000000000001": {ID: "d1", Name: "Smart Night Light Thread"}},
		HomeKit: map[string]haDevice{"43:3E:01:0F:92:53": {ID: "d2", Name: "Humidifier"}},
	}

	matched := matchHA(services, devices)
	var got []string
	for _, s := range matched {
		id := ""
		if s.ha != nil {
			id = s.ha.ID
		}
		got = append(got, s.name+" "+id)
	}
	if want := []string{"Smart Night Light Thread d1", "Humidifier d2", "Unpaired Lamp "}; !reflect.DeepEqual(got, want) {
		t.Errorf("services = %q", got)
	}
	if len(matchHA(services, nil)) != 4 {
		t.Error("without HA devices, every service should be kept")
	}
}
