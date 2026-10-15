package hydra

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hydra-admin/specs"
)

const Version26 = "v26.2.0"
const SpecSHA26 = "aa95a73bb14a90b8f2ce1f852cc41418cdbef3ac3bdedc09a48df0bc97a40540"

type Registry struct{}

type Registration struct {
	Tag              string
	SpecSHA256       string
	VersionPath      string
	ReadyPath        string
	ClientListPath   string
	ClientDetailPath string
}

var supported = map[string]Registration{
	Version26: {Tag: Version26, SpecSHA256: SpecSHA26, VersionPath: "/version", ReadyPath: "/health/ready", ClientListPath: "/admin/clients", ClientDetailPath: "/admin/clients/{id}"},
}

func NewRegistry() Registry { return Registry{} }

func (Registry) SupportedVersions() []string { return []string{Version26} }

// Lookup is deliberately closed: new release tags require a source change.
func (Registry) Lookup(version string) (Registration, bool) {
	registration, ok := supported[version]
	return registration, ok
}

// Match uses an exact release tag and checks both bytes and required operations.
func (Registry) Match(version string) bool {
	registration, ok := (Registry{}).Lookup(version)
	if !ok || registration.Tag != Version26 || registration.SpecSHA256 != SpecSHA26 {
		return false
	}
	digest := sha256.Sum256(specs.API26)
	if hex.EncodeToString(digest[:]) != SpecSHA26 {
		return false
	}
	var document struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Responses map[string]json.RawMessage `json:"responses"`
		} `json:"components"`
	}
	if json.Unmarshal(specs.API26, &document) != nil {
		return false
	}
	for _, path := range []string{registration.VersionPath, registration.ReadyPath, registration.ClientListPath, registration.ClientDetailPath} {
		method := "get"
		if len(document.Paths[path][method]) == 0 {
			return false
		}
	}
	var listResponse struct {
		Responses map[string]struct {
			Ref string `json:"$ref"`
		} `json:"responses"`
	}
	if json.Unmarshal(document.Paths[registration.ClientListPath]["get"], &listResponse) != nil || listResponse.Responses["200"].Ref != "#/components/responses/listOAuth2Clients" {
		return false
	}
	var listSchema struct {
		Content map[string]struct {
			Schema struct {
				Type  string `json:"type"`
				Items struct {
					Ref string `json:"$ref"`
				} `json:"items"`
			} `json:"schema"`
		} `json:"content"`
	}
	if json.Unmarshal(document.Components.Responses["listOAuth2Clients"], &listSchema) != nil || listSchema.Content["application/json"].Schema.Type != "array" || listSchema.Content["application/json"].Schema.Items.Ref != "#/components/schemas/oAuth2Client" {
		return false
	}
	var detailResponse struct {
		Responses map[string]struct {
			Content map[string]struct {
				Schema struct {
					Ref string `json:"$ref"`
				} `json:"schema"`
			} `json:"content"`
		} `json:"responses"`
	}
	if json.Unmarshal(document.Paths[registration.ClientDetailPath]["get"], &detailResponse) != nil || detailResponse.Responses["200"].Content["application/json"].Schema.Ref != "#/components/schemas/oAuth2Client" {
		return false
	}
	return true
}

func (r Registry) SpecValid() bool { return r.Match(Version26) }

// WriteValid binds the three mutations to the immutable tagged schema.
func (r Registry) WriteValid() bool {
	if !r.SpecValid() {
		return false
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if json.Unmarshal(specs.API26, &doc) != nil {
		return false
	}
	for _, v := range []struct{ path, method, needle string }{
		{"/admin/clients", "post", "#/components/schemas/oAuth2Client"},
		{"/admin/clients/{id}", "patch", "#/components/schemas/jsonPatchDocument"},
		{"/admin/clients/{id}", "delete", "204"},
	} {
		body := doc.Paths[v.path][v.method]
		if len(body) == 0 || !json.Valid(body) || !bytes.Contains(body, []byte(v.needle)) {
			return false
		}
	}
	return true
}

func (r Registry) TokenIntrospectValid() bool {
	if !r.SpecValid() {
		return false
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if json.Unmarshal(specs.API26, &doc) != nil {
		return false
	}
	body := doc.Paths["/admin/oauth2/introspect"]["post"]
	return bytes.Contains(body, []byte("application/x-www-form-urlencoded")) && bytes.Contains(body, []byte("#/components/schemas/introspectedOAuth2Token"))
}

func (r Registry) TokenRevokeValid() bool {
	if !r.TokenIntrospectValid() {
		return false
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if json.Unmarshal(specs.API26, &doc) != nil {
		return false
	}
	body := doc.Paths["/oauth2/revoke"]["post"]
	return bytes.Contains(body, []byte("application/x-www-form-urlencoded")) && bytes.Contains(body, []byte(`"basic"`)) && bytes.Contains(body, []byte("emptyResponse"))
}
