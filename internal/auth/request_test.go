package auth

import "testing"

func TestOriginAndTrustedProxy(t *testing.T) {
	base := "https://admin.example.test:8443"
	for _, pair := range [][2]string{{"", ""}, {"https://other.example.test", ""}, {"", "https://other.example.test/path"}, {"null", ""}} {
		if ValidateOrigin(pair[0], pair[1], base) {
			t.Fatalf("accepted origin %q referer %q", pair[0], pair[1])
		}
	}
	if !ValidateOrigin(base, "", base) || !ValidateOrigin("", base+"/login", base) {
		t.Fatal("same origin rejected")
	}
	if ValidateOrigin("https://admin.example.test", "", base) {
		t.Fatal("wrong port accepted")
	}
	ranges, err := ParseTrustedCIDRs([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	ip, scheme, err := ResolveClient("203.0.113.10:5000", false, "1.2.3.4", "https", ranges)
	if err != nil || ip.String() != "203.0.113.10" || scheme != "http" {
		t.Fatal("untrusted peer spoofed headers")
	}
	ip, scheme, err = ResolveClient("10.0.0.2:5000", false, "198.51.100.5, 10.0.0.1", "https", ranges)
	if err != nil || ip.String() != "198.51.100.5" || scheme != "https" {
		t.Fatalf("trusted chain failed: %v %s %v", ip, scheme, err)
	}
	ip, _, err = ResolveClient("10.0.0.2:5000", false, "1.2.3.4, 198.51.100.5", "", ranges)
	if err != nil || ip.String() != "198.51.100.5" {
		t.Fatal("walked past untrusted hop")
	}
	if _, _, err := ResolveClient("10.0.0.2:5000", false, "1.2.3.4", "http,https", ranges); err == nil {
		t.Fatal("accepted spoofable forwarded proto")
	}
}
