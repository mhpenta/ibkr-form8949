package washsale

import "testing"

func TestSecurityIdentityPreservesContract(t *testing.T) {
	for _, test := range []struct {
		description, identity string
		quantity              float64
	}{
		{"100 sh XYZ", "XYZ", 100},
		{"1,000.5 shares xyz", "XYZ", 1000.5},
		{"XYZ 50 sh", "XYZ", 50},
		{"  XYZ   INC  ", "XYZ INC", 0},
		{"XYZ 20SEP24 100 C", "XYZ 20SEP24 100 C", 0},
		{"XYZ 20SEP24 110 C", "XYZ 20SEP24 110 C", 0},
		{"XYZ 20SEP24 100 P", "XYZ 20SEP24 100 P", 0},
	} {
		identity, qty := Security(test.description)
		if identity != test.identity || qty != test.quantity {
			t.Errorf("%q = (%q, %g), want (%q, %g)", test.description, identity, qty, test.identity, test.quantity)
		}
	}
}
