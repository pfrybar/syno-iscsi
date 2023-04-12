package syno

import "testing"

func TestIsThin(t *testing.T) {
	toTest := map[int]bool{
		3:   false,
		15:  true,
		259: false,
		263: true,
		999: false,
	}

	for lunType, expected := range toTest {
		if IsThin(lunType) != expected {
			t.Errorf("IsThin(%d) - expected: %t, got %t", lunType, expected, !expected)
		}
	}
}

type lunTypeTest struct {
	FsType   string
	Thin     bool
	Expected string
}

func TestGetLunType(t *testing.T) {
	toTest := []lunTypeTest{
		{"ext4", false, "FILE"},
		{"ext4", true, "ADV"},
		{"btrfs", false, "BLUN_THICK"},
		{"btrfs", true, "BLUN"},
		{"none", false, ""},
	}

	for _, test := range toTest {
		out := GetLunType(test.FsType, test.Thin)
		if out != test.Expected {
			t.Errorf("GetLunType(\"%s\", %t) - expected: %s, got: %s", test.FsType, test.Thin, test.Expected, out)
		}
	}
}
