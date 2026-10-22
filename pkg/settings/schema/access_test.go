package schema

import "testing"

func TestMigrateAccess(t *testing.T) {
	cases := []struct {
		name string
		data string
		want bool
	}{
		{"old diag switch on", `{"diag":{"remote_adb":true}}`, true},
		{"old diag switch off", `{"diag":{"remote_adb":false}}`, false},
		{"new section wins", `{"access":{"adb":false},"diag":{"remote_adb":true}}`, false},
		{"nothing saved", `{}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var a Access
			if err := MigrateAccess([]byte(c.data), &a); err != nil {
				t.Fatalf("MigrateAccess(%s): %v", c.data, err)
			}
			if a.ADB != c.want {
				t.Errorf("MigrateAccess(%s).ADB = %v, want %v", c.data, a.ADB, c.want)
			}
		})
	}
}
