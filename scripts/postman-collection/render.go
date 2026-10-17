package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// The structs below spell out both file formats field by field, so the
// encoders write keys in a fixed order and a regenerated file differs from
// the committed one only where the spec did.

// v3 git-native YAML, one file per request, folder and example.

type v3Script struct {
	Type     string `yaml:"type"`
	Code     string `yaml:"code"`
	Language string `yaml:"language"`
}

type v3Definition struct {
	Kind        string     `yaml:"$kind"`
	Name        string     `yaml:"name,omitempty"`
	Description string     `yaml:"description,omitempty"`
	Scripts     []v3Script `yaml:"scripts,omitempty"`
	Order       int        `yaml:"order,omitempty"`
}

type v3Pair struct {
	Key         string `yaml:"key"`
	Value       string `yaml:"value"`
	Description string `yaml:"description,omitempty"`
}

type v3Body struct {
	Type    string `yaml:"type"`
	Content any    `yaml:"content"`
}

type v3Settings struct {
	FollowRedirects bool `yaml:"followRedirects"`
}

type v3Request struct {
	Kind        string      `yaml:"$kind"`
	Name        string      `yaml:"name,omitempty"`
	Description string      `yaml:"description,omitempty"`
	Method      string      `yaml:"method"`
	URL         string      `yaml:"url"`
	Headers     []v3Pair    `yaml:"headers,omitempty"`
	QueryParams []v3Pair    `yaml:"queryParams,omitempty"`
	Body        *v3Body     `yaml:"body,omitempty"`
	Settings    *v3Settings `yaml:"settings,omitempty"`
	Scripts     []v3Script  `yaml:"scripts,omitempty"`
	Examples    string      `yaml:"examples,omitempty"`
	Order       int         `yaml:"order"`
}

type v3Example struct {
	Kind    string `yaml:"$kind"`
	Request struct {
		URL    string `yaml:"url"`
		Method string `yaml:"method"`
	} `yaml:"request"`
	Response struct {
		StatusCode int      `yaml:"statusCode"`
		StatusText string   `yaml:"statusText"`
		Headers    []v3Pair `yaml:"headers"`
		Body       v3Body   `yaml:"body"`
	} `yaml:"response"`
	Order int `yaml:"order"`
}

type v3EnvironmentValue struct {
	Key     string `yaml:"key"`
	Value   string `yaml:"value"`
	Enabled bool   `yaml:"enabled"`
	Type    string `yaml:"type"`
}

type v3Environment struct {
	Name   string               `yaml:"name"`
	Values []v3EnvironmentValue `yaml:"values"`
}

func encodeYAML(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func scripts(preRequest, test, preType, testType string) []v3Script {
	var out []v3Script
	if preRequest != "" {
		out = append(out, v3Script{Type: preType, Code: preRequest, Language: javascript})
	}
	if test != "" {
		out = append(out, v3Script{Type: testType, Code: test, Language: javascript})
	}
	return out
}

func renderV3(model collection) (map[string][]byte, error) {
	files := map[string][]byte{}
	put := func(name string, value any) error {
		content, err := encodeYAML(value)
		if err != nil {
			return err
		}
		files[name] = content
		return nil
	}
	root := path.Join("collections", model.name)
	if err := put(path.Join(root, ".resources", "definition.yaml"), v3Definition{
		Kind:        "collection",
		Name:        model.name,
		Description: model.description,
		Scripts:     scripts(model.preRequest, model.test, "http:beforeRequest", "http:afterResponse"),
	}); err != nil {
		return nil, err
	}

	for folderIndex, f := range model.folders {
		dir := path.Join(root, fileName(f.name))
		if err := put(path.Join(dir, ".resources", "definition.yaml"), v3Definition{
			Kind:        "collection",
			Name:        nameIfChanged(f.name),
			Description: f.description,
			Order:       (folderIndex + 1) * 1000,
		}); err != nil {
			return nil, err
		}
		for requestIndex, r := range f.requests {
			if err := renderRequest(dir, (requestIndex+1)*1000, r, put); err != nil {
				return nil, err
			}
		}
	}

	values := make([]v3EnvironmentValue, len(environment))
	for i, v := range environment {
		values[i] = v3EnvironmentValue{Key: v.key, Value: v.value, Enabled: true, Type: v.kind}
	}
	if err := put(path.Join("environments", environmentName+".yaml"), v3Environment{Name: environmentName, Values: values}); err != nil {
		return nil, err
	}
	return files, nil
}

// renderRequest writes one request file and its examples under dir.
func renderRequest(dir string, order int, r request, put func(string, any) error) error {
	stem := fileName(r.name)
	file := v3Request{
		Kind:        "http-request",
		Name:        nameIfChanged(r.name),
		Description: r.description,
		Method:      r.method,
		URL:         r.url(),
		Scripts:     scripts(r.preRequest, r.test, "beforeRequest", "afterResponse"),
		Order:       order,
	}
	for _, q := range r.query {
		file.QueryParams = append(file.QueryParams, v3Pair{Key: q.key, Value: q.value, Description: q.description})
	}
	if r.body != nil {
		switch r.body.mode {
		case bodyJSON:
			file.Headers = []v3Pair{{Key: contentType, Value: "application/json"}}
			file.Body = &v3Body{Type: bodyJSON, Content: r.body.json}
		case bodyURLEncoded:
			form := make([]v3Pair, len(r.body.form))
			for i, field := range r.body.form {
				form[i] = v3Pair{Key: field[0], Value: field[1]}
			}
			file.Body = &v3Body{Type: bodyURLEncoded, Content: form}
		}
	}
	if r.noRedirect {
		file.Settings = &v3Settings{FollowRedirects: false}
	}
	if len(r.examples) > 0 {
		examplesDir := path.Join(".resources", stem+".resources", "examples")
		file.Examples = "./" + examplesDir
		for exampleIndex, e := range r.examples {
			var out v3Example
			out.Kind = "http-example"
			out.Request.URL = r.url()
			out.Request.Method = r.method
			out.Response.StatusCode = e.status
			out.Response.StatusText = http.StatusText(e.status)
			out.Response.Headers = []v3Pair{{Key: contentType, Value: e.contentType}}
			out.Response.Body = v3Body{Type: exampleBodyType(e.contentType), Content: e.body}
			out.Order = (exampleIndex + 1) * 1000
			if err := put(path.Join(dir, examplesDir, fileName(e.name)+".example.yaml"), out); err != nil {
				return err
			}
		}
	}
	return put(path.Join(dir, stem+".request.yaml"), file)
}

// nameIfChanged returns the name when its file name had to change, which is
// the only time the v3 format wants it spelled out in the file.
func nameIfChanged(name string) string {
	if fileName(name) == name {
		return ""
	}
	return name
}

func exampleBodyType(mediaType string) string {
	switch {
	case strings.Contains(mediaType, bodyJSON):
		return bodyJSON
	case strings.Contains(mediaType, "html"):
		return "html"
	default:
		return "text"
	}
}

// v2.1 JSON, the single file the Postman app imports.

type v21Script struct {
	Type string   `json:"type"`
	Exec []string `json:"exec"`
}

type v21Event struct {
	Listen string    `json:"listen"`
	Script v21Script `json:"script"`
}

type v21Pair struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

type v21URL struct {
	Raw   string    `json:"raw"`
	Host  []string  `json:"host"`
	Path  []string  `json:"path"`
	Query []v21Pair `json:"query,omitempty"`
}

type v21Body struct {
	Mode       string    `json:"mode"`
	Raw        string    `json:"raw,omitempty"`
	URLEncoded []v21Pair `json:"urlencoded,omitempty"`
	Options    any       `json:"options,omitempty"`
}

type v21Request struct {
	Method      string    `json:"method"`
	Header      []v21Pair `json:"header"`
	Body        *v21Body  `json:"body,omitempty"`
	URL         v21URL    `json:"url"`
	Description string    `json:"description,omitempty"`
}

type v21Response struct {
	Name            string    `json:"name"`
	OriginalRequest any       `json:"originalRequest"`
	Status          string    `json:"status"`
	Code            int       `json:"code"`
	PreviewLanguage string    `json:"_postman_previewlanguage"`
	Header          []v21Pair `json:"header"`
	Body            string    `json:"body"`
}

type v21Item struct {
	Name                    string        `json:"name"`
	Description             string        `json:"description,omitempty"`
	Item                    []v21Item     `json:"item,omitempty"`
	Event                   []v21Event    `json:"event,omitempty"`
	ProtocolProfileBehavior any           `json:"protocolProfileBehavior,omitempty"`
	Request                 *v21Request   `json:"request,omitempty"`
	Response                []v21Response `json:"response,omitempty"`
}

type v21Collection struct {
	Info struct {
		PostmanID   string `json:"_postman_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Schema      string `json:"schema"`
	} `json:"info"`
	Item  []v21Item  `json:"item"`
	Event []v21Event `json:"event"`
}

type v21EnvironmentValue struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
}

type v21Environment struct {
	ID     string                `json:"id"`
	Name   string                `json:"name"`
	Values []v21EnvironmentValue `json:"values"`
	Scope  string                `json:"_postman_variable_scope"`
}

func events(preRequest, test string) []v21Event {
	var out []v21Event
	if preRequest != "" {
		out = append(out, v21Event{Listen: "prerequest", Script: v21Script{Type: javascript, Exec: strings.Split(preRequest, "\n")}})
	}
	if test != "" {
		out = append(out, v21Event{Listen: "test", Script: v21Script{Type: javascript, Exec: strings.Split(test, "\n")}})
	}
	return out
}

func v21URLOf(r request) v21URL {
	u := v21URL{Raw: r.url(), Host: []string{"{{base_url}}"}}
	for segment := range strings.SplitSeq(strings.TrimPrefix(r.path, "/"), "/") {
		u.Path = append(u.Path, segment)
	}
	for _, q := range r.query {
		u.Query = append(u.Query, v21Pair{Key: q.key, Value: q.value, Description: q.description})
	}
	return u
}

func renderV21(model collection) (map[string][]byte, error) {
	var out v21Collection
	// Fixed rather than random, so a re-import replaces the collection in
	// Postman instead of adding a second copy, and regenerating is stable.
	out.Info.PostmanID = "6f8e3c2a-1b4d-4e5f-9a7b-0c1d2e3f4a5b"
	out.Info.Name = model.name
	out.Info.Description = model.description
	out.Info.Schema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
	out.Event = events(model.preRequest, model.test)
	for _, f := range model.folders {
		item := v21Item{Name: f.name, Description: f.description}
		for _, r := range f.requests {
			req := &v21Request{Method: r.method, Header: []v21Pair{}, URL: v21URLOf(r), Description: r.description}
			if r.body != nil {
				switch r.body.mode {
				case bodyJSON:
					req.Header = append(req.Header, v21Pair{Key: contentType, Value: "application/json"})
					req.Body = &v21Body{Mode: "raw", Raw: r.body.json, Options: map[string]any{"raw": map[string]string{"language": bodyJSON}}}
				case bodyURLEncoded:
					form := make([]v21Pair, len(r.body.form))
					for i, field := range r.body.form {
						form[i] = v21Pair{Key: field[0], Value: field[1], Type: "text"}
					}
					req.Body = &v21Body{Mode: bodyURLEncoded, URLEncoded: form}
				}
			}
			entry := v21Item{Name: r.name, Event: events(r.preRequest, r.test), Request: req}
			if r.noRedirect {
				entry.ProtocolProfileBehavior = map[string]bool{"followRedirects": false}
			}
			for _, e := range r.examples {
				entry.Response = append(entry.Response, v21Response{
					Name:            e.name,
					OriginalRequest: map[string]any{"method": r.method, "header": []v21Pair{}, "url": v21URLOf(r)},
					Status:          http.StatusText(e.status),
					Code:            e.status,
					PreviewLanguage: exampleBodyType(e.contentType),
					Header:          []v21Pair{{Key: contentType, Value: e.contentType}},
					Body:            e.body,
				})
			}
			item.Item = append(item.Item, entry)
		}
		out.Item = append(out.Item, item)
	}

	env := v21Environment{ID: "2c7d9e1f-3a4b-4c5d-8e6f-7a8b9c0d1e2f", Name: environmentName, Scope: "environment"}
	for _, v := range environment {
		env.Values = append(env.Values, v21EnvironmentValue{Key: v.key, Value: v.value, Type: v.kind, Enabled: true})
	}

	files := map[string][]byte{}
	for name, value := range map[string]any{
		"export/openplan-api.postman_collection.json":    out,
		"export/openplan-local.postman_environment.json": env,
	} {
		content, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return nil, err
		}
		files[name] = content
	}
	return files, nil
}

type environmentValue struct {
	key, value, kind string
}

// Postman's two variable kinds: a secret is masked in the app.
const (
	plainVariable  = "default"
	secretVariable = "secret"
)

// environment is every variable the collection reads. The ids start empty and
// are filled in as a run creates things; session_cookie is a secret the user
// pastes in, and never belongs in this file.
var environment = []environmentValue{
	{"base_url", "http://localhost:8081", plainVariable},
	{"tenant_id", "tenant_123", plainVariable},
	{"session_cookie", "", secretVariable},
	{"template_repo_owner", "openplanhq", plainVariable},
	{"template_repo_name", "demo-templates", plainVariable},
	{"template_source_ref", "main", plainVariable},
	{"template_root_path", "modules/random-id", plainVariable},
	{"user_search", "a", plainVariable},
	{"my_sub", "", plainVariable},
	{"registration_id", "", plainVariable},
	{"template_revision_id", "", plainVariable},
	{"stack_id", "", plainVariable},
	{"stack_template_id", "", plainVariable},
	{"credential_id", "", plainVariable},
	{"run_id", "", plainVariable},
	{"phase", "", plainVariable},
	{"user_sub", "", plainVariable},
}
