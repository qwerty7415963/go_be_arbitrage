package telegram

import (
	"testing"
)

func TestGenerateCode_LengthAndAlphabet(t *testing.T) {
	for i := 0; i < 100; i++ {
		c, err := GenerateCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(c) != CodeLength {
			t.Fatalf("len %d", len(c))
		}
		if err := ValidateCode(c); err != nil {
			t.Fatalf("invalid code %s: %v", c, err)
		}
	}
}

func TestValidateCode_Invalid(t *testing.T) {
	if err := ValidateCode("0O1IABCD"); err == nil {
		t.Fatal("expected invalid")
	}
	if err := ValidateCode("SHORT"); err == nil {
		t.Fatal("expected invalid")
	}
}

func TestLocale_IsValid(t *testing.T) {
	if !LocaleVI.IsValid() || !LocaleEN.IsValid() {
		t.Fatal("valid locales")
	}
	if Locale("fr").IsValid() {
		t.Fatal("invalid locale accepted")
	}
}
