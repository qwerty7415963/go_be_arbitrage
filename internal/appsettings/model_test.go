package appsettings

import (
	"testing"
)

func TestTelegramSettings_Validate(t *testing.T) {
	cases := []struct {
		v  int
		ok bool
	}{
		{0, false},
		{1001, false},
		{1, true},
		{1000, true},
		{100, true},
	}
	for _, c := range cases {
		err := (TelegramSettings{MaxWalletsPerUser: c.v}).Validate()
		if c.ok && err != nil {
			t.Errorf("value %d expected ok, got %v", c.v, err)
		}
		if !c.ok && err == nil {
			t.Errorf("value %d expected error", c.v)
		}
	}
}
