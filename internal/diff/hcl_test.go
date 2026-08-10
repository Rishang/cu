package diff

import (
	"reflect"
	"testing"
)

// Go's HCL parser evaluates literals, so strings arrive as real strings and
// labelled blocks nest by label. python-hcl2 instead kept the surrounding
// quote characters ('"us-east-1"') and made blocks a list of single-key maps,
// which is why the paths differ from the Python version.
func TestLoadTFVars(t *testing.T) {
	data, err := LoadFile(asset("prod.tfvars"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	vars, ok := normalize(data).(map[string]any)
	if !ok {
		t.Fatalf("got %T, want a map", normalize(data))
	}

	for _, key := range []string{"region", "instance_type", "replica_count", "enable_dns", "tags"} {
		if _, present := vars[key]; !present {
			t.Errorf("missing key %q", key)
		}
	}

	if vars["region"] != "us-east-1" {
		t.Errorf("region = %#v, want \"us-east-1\" without embedded quotes", vars["region"])
	}
	if vars["replica_count"] != int64(2) {
		t.Errorf("replica_count = %#v (%T), want int64(2)", vars["replica_count"], vars["replica_count"])
	}
	if vars["enable_dns"] != true {
		t.Errorf("enable_dns = %#v, want true", vars["enable_dns"])
	}

	tags, ok := vars["tags"].(map[string]any)
	if !ok {
		t.Fatalf("tags = %T, want a map", vars["tags"])
	}
	if tags["env"] != "prod" {
		t.Errorf("tags.env = %#v, want \"prod\"", tags["env"])
	}
}

func TestLoadHCLExtension(t *testing.T) {
	got, err := LoadFile(write(t, "config.hcl", "key = \"value\"\n"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !reflect.DeepEqual(normalize(got), map[string]any{"key": "value"}) {
		t.Fatalf("got %#v", normalize(got))
	}
}

func TestLoadHCLInvalid(t *testing.T) {
	_, err := LoadFile(write(t, "bad.tfvars", "this is not { valid hcl !!!"))
	assertErrContains(t, err, "Invalid HCL")
}

func TestLoadTFBlocks(t *testing.T) {
	data, err := LoadFile(asset("infra-a.tf"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	root, ok := normalize(data).(map[string]any)
	if !ok {
		t.Fatalf("got %T, want a map", normalize(data))
	}

	for _, key := range []string{"variable", "resource"} {
		if _, present := root[key]; !present {
			t.Fatalf("missing top-level key %q", key)
		}
	}

	// variable "region" { default = "us-east-1" } → variable.region[0].default
	variables := root["variable"].(map[string]any)
	region, ok := variables["region"].([]any)
	if !ok {
		t.Fatalf("variable.region = %T, want a list of blocks", variables["region"])
	}
	if got := region[0].(map[string]any)["default"]; got != "us-east-1" {
		t.Errorf("variable.region[0].default = %#v, want \"us-east-1\"", got)
	}
}

func TestDiffTFVars(t *testing.T) {
	a, err := LoadFile(asset("prod.tfvars"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	b, err := LoadFile(asset("stage.tfvars"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	entries := Compute(a, b)
	if len(entries) == 0 {
		t.Fatal("expected differences between prod and stage tfvars")
	}

	got := map[string]Kind{}
	for _, e := range entries {
		got[e.PathStr()] = e.Kind
	}
	for _, path := range []string{"region", "enable_dns", "instance_type", "replica_count"} {
		if got[path] != KindChanged {
			t.Errorf("%s: kind = %q, want changed", path, got[path])
		}
	}
}

func TestDiffTFVarsNoFalsePositives(t *testing.T) {
	content := "region = \"us-east-1\"\ncount = 2\n"
	a, err := LoadFile(write(t, "a.tfvars", content))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	b, err := LoadFile(write(t, "b.tfvars", content))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if entries := Compute(a, b); len(entries) != 0 {
		t.Fatalf("identical files reported %d differences: %+v", len(entries), entries)
	}
}

func TestDiffTFVarsNewKey(t *testing.T) {
	a, err := LoadFile(write(t, "a.tfvars", "region = \"us-east-1\"\n"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	b, err := LoadFile(write(t, "b.tfvars", "region = \"us-east-1\"\nextra = \"new\"\n"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	entries := Compute(a, b)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(entries), entries)
	}
	if entries[0].Kind != KindAdded || entries[0].PathStr() != "extra" {
		t.Fatalf("got %s at %s, want added at extra", entries[0].Kind, entries[0].PathStr())
	}
}

func TestDiffTFBlocksProducesReadablePaths(t *testing.T) {
	a, err := LoadFile(asset("infra-a.tf"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	b, err := LoadFile(asset("infra-b.tf"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	got := paths(Compute(a, b))
	want := []string{
		"variable.region[0].default",
		"resource.aws_instance.web[0].instance_type",
		"resource.aws_s3_bucket.data[0].force_destroy",
	}
	for _, path := range want {
		if !got[path] {
			t.Errorf("missing %q in %v", path, got)
		}
	}
}
