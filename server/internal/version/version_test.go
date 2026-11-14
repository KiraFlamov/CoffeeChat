package version

import "testing"

func TestString(t *testing.T) {
	tests := []struct {
		name   string
		commit string
		want   string
	}{
		{name: "локальная сборка", commit: "", want: "dev"},
		{name: "сборка с хэшем", commit: "abc1234", want: "abc1234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := commit
			t.Cleanup(func() { commit = old })
			commit = tt.commit
			if got := String(); got != tt.want {
				t.Errorf("String() = %q, хотели %q", got, tt.want)
			}
		})
	}
}
