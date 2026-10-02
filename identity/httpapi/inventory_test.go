package httpapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
	"github.com/sre-norns/wyrd/identity/resource"
)

// The checked-in inventory is the reviewable coverage boundary. Adding a route
// requires declaring its response type and whether its input is a resource or
// an explicit command/query exception.
func TestEveryMountedResourceRouteIsClassified(t *testing.T) {
	data, err := os.ReadFile("../docs/routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct{ Method, Path, Response, Input string }
	if err = json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, d := range resource.Definitions() {
		known[d.Model] = true
	}
	inventory := map[string]bool{}
	for _, row := range rows {
		key := row.Method + " " + row.Path
		if inventory[key] {
			t.Fatalf("duplicate inventory %s", key)
		}
		inventory[key] = true
		if row.Input != "none" && row.Input != "create" && row.Input != "command" && row.Input != "patch/command" {
			t.Errorf("unclassified input %s", key)
		}
		for _, typ := range strings.Split(row.Response, "|") {
			if !known[typ] && !strings.HasPrefix(typ, "query:") {
				t.Errorf("unclassified output %s: %s", key, typ)
			}
		}
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Mount(router, server.NewService(nil), Config{})
	for _, route := range router.Routes() {
		if !strings.HasPrefix(route.Path, "/v1/") {
			continue
		}
		key := route.Method + " " + route.Path
		if !inventory[key] {
			t.Errorf("unclassified mounted route %s", key)
		}
		delete(inventory, key)
	}
	for key := range inventory {
		t.Errorf("inventory route is not mounted: %s", key)
	}
}
