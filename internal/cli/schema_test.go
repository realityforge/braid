package cli

import "testing"

func TestSchemaIsValid(t *testing.T) {
	if err := ValidateSchema(); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaCoversEveryCommand(t *testing.T) {
	want := []Command{
		CommandAdd,
		CommandPull,
		CommandRemove,
		CommandDiff,
		CommandPush,
		CommandSync,
		CommandVersion,
		CommandStatus,
		CommandCompletion,
		CommandComplete,
		CommandUpgradeConfig,
	}
	for _, command := range want {
		if _, ok := CommandSpecForCommand(command); !ok {
			t.Errorf("schema missing command %s", command)
		}
	}
	if got := len(CommandSpecs()); got != len(want) {
		t.Fatalf("schema has %d commands, want %d", got, len(want))
	}
}

func TestSchemaCommandNamesResolveCanonically(t *testing.T) {
	for _, spec := range CommandSpecs() {
		for _, name := range append([]string{spec.Name}, spec.Aliases...) {
			got, ok := CommandSpecForName(name)
			if !ok {
				t.Errorf("schema name %q does not resolve", name)
				continue
			}
			if got.Command != spec.Command {
				t.Errorf("schema name %q resolves to %s, want %s", name, got.Command, spec.Command)
			}
		}
	}
}
