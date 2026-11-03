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

func TestMatterNames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if r.Method != http.MethodPost || r.URL.Path != "/template" || r.Header.Get("Authorization") != "Bearer tok" || !strings.Contains(body["template"], "MatterNodeDevice") {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"74F6D4610D01F2BA-0000000000000001":"Smart Night Light Thread"}`))
	}))
	defer srv.Close()

	got := matterNames(context.Background(), srv.URL, "tok")
	if want := map[string]string{"74F6D4610D01F2BA-0000000000000001": "Smart Night Light Thread"}; !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v", got)
	}
	if matterNames(context.Background(), srv.URL, "wrong") != nil || matterNames(context.Background(), srv.URL, "") != nil {
		t.Error("expected nil when HA can't be asked")
	}
}

func TestNameMatter(t *testing.T) {
	services := []service{
		{name: "74F6D4610D01F2BA-0000000000000001", matter: true},
		{name: "459FCEB0616EBF74-000000006C54EB63", matter: true}, // Another controller's fabric.
		{name: "Airversa AH1 2122"},
	}
	names := map[string]string{"74F6D4610D01F2BA-0000000000000001": "Smart Night Light Thread"}

	var got []string
	for _, s := range nameMatter(services, names) {
		got = append(got, s.name)
	}
	if want := []string{"Smart Night Light Thread", "Airversa AH1 2122"}; !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v", got)
	}
	if len(nameMatter(services, nil)) != 3 {
		t.Error("without HA names, every service should be kept")
	}
}
