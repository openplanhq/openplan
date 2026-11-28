package main

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	collectionName  = "openplan API"
	environmentName = "openplan local"

	bodyJSON       = "json"
	bodyURLEncoded = "urlencoded"
	contentType    = "Content-Type"
	javascript     = "text/javascript"
)

// collection is the format-neutral model both renderers read.
type collection struct {
	name        string
	description string
	preRequest  string
	test        string
	folders     []folder
}

type folder struct {
	name        string
	description string
	requests    []request
}

type request struct {
	name        string
	description string
	method      string
	path        string // after {{base_url}}, with path parameters as {{name}}
	query       []queryParam
	body        *body
	preRequest  string
	test        string
	noRedirect  bool
	examples    []example
}

type queryParam struct {
	key, value, description string
}

type body struct {
	mode string // bodyJSON or bodyURLEncoded
	json string
	form [][2]string
}

type example struct {
	name        string
	status      int
	contentType string
	body        string
}

// url renders the request URL, query string included, as both Postman
// formats expect it.
func (r request) url() string {
	u := "{{base_url}}" + r.path
	if len(r.query) > 0 {
		parts := make([]string, 0, len(r.query))
		for _, q := range r.query {
			parts = append(parts, q.key+"="+q.value)
		}
		u += "?" + strings.Join(parts, "&")
	}
	return u
}

var pathParameter = regexp.MustCompile(`\{([a-z_]+)\}`)

// buildCollection reads every operation in the spec, in document order, into
// the folder of its first tag.
func buildCollection(doc *specDoc) (collection, error) {
	byTag := map[string]*folder{}
	var tagOrder []string
	for _, tag := range get(doc.root, "tags").Content {
		name := str(get(tag, "name"))
		byTag[name] = &folder{name: name, description: str(get(tag, "description"))}
		tagOrder = append(tagOrder, name)
	}

	seen := map[string]bool{}
	for _, pathEntry := range pairs(get(doc.root, "paths")) {
		path, pathItem := pathEntry[0].Value, pathEntry[1]
		pathParams := get(pathItem, "parameters")
		for _, opEntry := range pairs(pathItem) {
			method := strings.ToUpper(opEntry[0].Value)
			if !isMethod(method) {
				continue
			}
			op := opEntry[1]
			operationID := str(get(op, "operationId"))
			if operationID == "" {
				return collection{}, fmt.Errorf("%s %s has no operationId", method, path)
			}
			seen[operationID] = true
			req, err := buildRequest(doc, method, path, pathParams, op, overrides[operationID])
			if err != nil {
				return collection{}, fmt.Errorf("%s: %w", operationID, err)
			}
			tags := get(op, "tags")
			if tags == nil || len(tags.Content) == 0 {
				return collection{}, fmt.Errorf("%s has no tag", operationID)
			}
			target, ok := byTag[tags.Content[0].Value]
			if !ok {
				return collection{}, fmt.Errorf("%s: tag %q is not declared", operationID, tags.Content[0].Value)
			}
			target.requests = append(target.requests, req)
		}
	}
	// An override for an operation the spec no longer has would silently do
	// nothing; say so instead.
	for operationID := range overrides {
		if !seen[operationID] {
			return collection{}, fmt.Errorf("override for unknown operation %q", operationID)
		}
	}

	model := collection{
		name:        collectionName,
		description: collectionDescription,
		preRequest:  collectionPreRequest,
		test:        collectionTest,
	}
	for _, name := range orderFolders(tagOrder) {
		model.folders = append(model.folders, *byTag[name])
	}
	// The contract checks go before sign-in and sign-out, which ends the
	// session everything else uses.
	last := model.folders[len(model.folders)-1]
	model.folders = append(model.folders[:len(model.folders)-1], contractChecks(), last)
	return model, nil
}

// orderFolders runs the tags in the order a collection run needs: each folder
// creates what a later one reads. Searching users comes before granting a
// role to one, and signing in and out comes last, since signing out revokes
// the session the run uses.
func orderFolders(tags []string) []string {
	rank := map[string]int{"Users": 1, "Access": 2, "Queue": 3, "Authentication": 4}
	ordered := append([]string(nil), tags...)
	sort.SliceStable(ordered, func(i, j int) bool { return rank[ordered[i]] < rank[ordered[j]] })
	return ordered
}

func isMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func buildRequest(doc *specDoc, method, path string, pathParams *yaml.Node, op *yaml.Node, extra override) (request, error) {
	req := request{
		name:        str(get(op, "summary")),
		description: strings.TrimSpace(str(get(op, "description"))),
		method:      method,
		path:        pathParameter.ReplaceAllString(path, "{{$1}}"),
		noRedirect:  extra.noRedirect,
	}
	for parameter, variable := range extra.pathVars {
		req.path = strings.ReplaceAll(req.path, "{{"+parameter+"}}", "{{"+variable+"}}")
	}
	if req.name == "" {
		return request{}, fmt.Errorf("%s %s has no summary", method, path)
	}
	if extra.note != "" {
		req.description = strings.TrimSpace(req.description + "\n\n" + extra.note)
	}

	query, err := queryParams(doc, pathParams, get(op, "parameters"), extra.query)
	if err != nil {
		return request{}, err
	}
	req.query = query

	switch {
	case extra.noBody:
	case extra.jsonBody != "":
		req.body = &body{mode: bodyJSON, json: extra.jsonBody}
	case extra.formBody != nil:
		req.body = &body{mode: bodyURLEncoded, form: extra.formBody}
	default:
		requestJSON := get(get(get(get(op, "requestBody"), "content"), "application/json"), "example")
		if requestJSON != nil {
			rendered, err := nodeJSON(requestJSON)
			if err != nil {
				return request{}, err
			}
			req.body = &body{mode: bodyJSON, json: rendered}
		}
	}

	responses := get(op, "responses")
	expected := extra.expect
	if expected == nil {
		expected = []int{successStatus(responses)}
	}
	if expected[0] == 0 {
		return request{}, fmt.Errorf("%s %s documents no 2xx or 3xx response", method, path)
	}
	req.test = requestTest(expected, extra.test)
	req.preRequest = extra.preRequest

	examples, err := responseExamples(responses)
	if err != nil {
		return request{}, err
	}
	req.examples = examples
	return req, nil
}

// successStatus is the lowest 2xx status the operation documents, or the
// lowest 3xx for a redirecting sign-in route.
func successStatus(responses *yaml.Node) int {
	best := 0
	for _, prefix := range []string{"2", "3"} {
		for _, entry := range pairs(responses) {
			code, err := strconv.Atoi(entry[0].Value)
			if err != nil || !strings.HasPrefix(entry[0].Value, prefix) {
				continue
			}
			if best == 0 || code < best {
				best = code
			}
		}
		if best != 0 {
			return best
		}
	}
	return 0
}

// queryParams takes the operation's query parameters, path-level ones first:
// required ones, and ones with a default, valued from the override, the
// default, or an environment variable of the same name.
func queryParams(doc *specDoc, pathParams, opParams *yaml.Node, values map[string]string) ([]queryParam, error) {
	var nodes []*yaml.Node
	for _, list := range []*yaml.Node{pathParams, opParams} {
		if list != nil {
			nodes = append(nodes, list.Content...)
		}
	}
	var out []queryParam
	for _, raw := range nodes {
		param, err := doc.resolve(raw)
		if err != nil {
			return nil, err
		}
		if str(get(param, "in")) != "query" {
			continue
		}
		name := str(get(param, "name"))
		value, overridden := values[name]
		def := get(get(param, "schema"), "default")
		if !overridden && def == nil && str(get(param, "required")) != "true" {
			continue
		}
		switch {
		case overridden:
		case def != nil:
			value = def.Value
		default:
			value = "{{" + name + "}}"
		}
		out = append(out, queryParam{key: name, value: value, description: strings.TrimSpace(str(get(param, "description")))})
	}
	return out, nil
}

// responseExamples are the responses an operation documents itself with an
// explicit example, in status order. Responses described only by a schema get
// none, since an example invented from a schema says nothing the schema does
// not; nor do the shared ones it references (401, 404, 500 and the like),
// which would repeat the same envelope on every request.
func responseExamples(responses *yaml.Node) ([]example, error) {
	entries := pairs(responses)
	sort.SliceStable(entries, func(i, j int) bool { return entries[i][0].Value < entries[j][0].Value })
	var out []example
	names := map[string]bool{}
	for _, entry := range entries {
		status, err := strconv.Atoi(entry[0].Value)
		if err != nil || get(entry[1], "$ref") != nil {
			continue
		}
		for _, mediaEntry := range pairs(get(entry[1], "content")) {
			found, err := mediaExamples(status, mediaEntry[0].Value, mediaEntry[1], names)
			if err != nil {
				return nil, err
			}
			out = append(out, found...)
		}
	}
	return out, nil
}

// mediaExamples reads one media type's `example` and named `examples`. names
// holds the names already used, so two examples never share a file.
func mediaExamples(status int, mediaType string, media *yaml.Node, names map[string]bool) ([]example, error) {
	type labelledNode struct {
		label string
		value *yaml.Node
	}
	var labelled []labelledNode
	if value := get(media, "example"); value != nil {
		labelled = append(labelled, labelledNode{http.StatusText(status), value})
	}
	for _, named := range pairs(get(media, "examples")) {
		if value := get(named[1], "value"); value != nil {
			labelled = append(labelled, labelledNode{named[0].Value, value})
		}
	}
	out := make([]example, 0, len(labelled))
	for _, entry := range labelled {
		value := entry.value
		content := value.Value
		if strings.HasSuffix(mediaType, bodyJSON) {
			rendered, err := nodeJSON(value)
			if err != nil {
				return nil, err
			}
			content = rendered
		}
		name := fmt.Sprintf("%d %s", status, entry.label)
		for names[strings.ToLower(name)] {
			name += " again"
		}
		names[strings.ToLower(name)] = true
		out = append(out, example{name: name, status: status, contentType: mediaType, body: content})
	}
	return out, nil
}

// fileName makes a request or example name safe as a file name stem, as the
// v3 format requires.
func fileName(name string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, name)
}
