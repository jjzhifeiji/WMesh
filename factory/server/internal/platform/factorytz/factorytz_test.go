package factorytz

import "testing"

// 没配、配错和 Local 都退回东八区，配对了就用配的。
func TestLoad(t *testing.T) {
	// 逐条核对入参和应得的时区名。
	for raw, want := range map[string]string{"": Default, "  ": Default, "Nope/Zone": Default, "Local": Default, "UTC": "UTC", "Europe/Berlin": "Europe/Berlin"} {
		// 名字不对就停。
		if got := Load(raw).String(); got != want {
			// 配错没退回默认，或配对了没用上。
			t.Fatalf("Load(%q) = %q want %q", raw, got, want)
		}
	}
}
