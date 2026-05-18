package plugin

import "testing"

func TestValidateManifestCommands(t *testing.T) {
	t.Parallel()

	valid := Manifest{
		Name: "demo",
		Commands: []CommandDecl{{
			Name:        "inspect",
			Description: "Inspect the plugin",
			Exec:        "./bin/inspect",
		}},
	}
	if errs := ValidateManifest(valid); len(errs) != 0 {
		t.Fatalf("ValidateManifest(valid commands) errors = %#v", errs)
	}

	invalid := Manifest{
		Name: "demo",
		Commands: []CommandDecl{{
			Description: "Missing everything important",
		}},
	}
	errs := ValidateManifest(invalid)
	if len(errs) != 2 {
		t.Fatalf("ValidateManifest(invalid commands) len = %d, want 2 (%#v)", len(errs), errs)
	}
}
