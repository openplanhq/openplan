package app

import "testing"

func TestSlugIsDerived(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, stackName, slug string
		want                  bool
	}{
		{"defaulted from a name with a space", "Edge CDN", "edge-cdn", true},
		{"chosen and different from the name", "Shared networking", "net-core", false},
		{"chosen but equal to the derived slug", "Shared networking", "shared-networking", true},
		{"name already a slug", "prod", "prod", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := SlugIsDerived(test.stackName, test.slug); got != test.want {
				t.Fatalf("SlugIsDerived(%q, %q) = %v, want %v", test.stackName, test.slug, got, test.want)
			}
		})
	}
}
