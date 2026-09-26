package main

import "testing"

func TestLocalAccessEnvironment(t *testing.T) {
	for _, row := range []struct {
		raw           string
		want, invalid bool
	}{
		{"", false, false}, {"0", false, false}, {"false", false, false},
		{"1", true, false}, {"true", true, false}, {"yes", false, true},
	} {
		t.Run(row.raw, func(t *testing.T) {
			t.Setenv("JPACK_DESK_LOCAL_ACCESS", row.raw)
			value, err := localAccessFromEnv()
			if value != row.want || (err != nil) != row.invalid {
				t.Fatalf("value=%v, err=%v", value, err)
			}
		})
	}
}
